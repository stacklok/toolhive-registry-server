package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stacklok/toolhive/pkg/logger"
	"golang.org/x/term"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

const (
	// Fixed role name for the registry server
	fixedRoleName = "toolhive_registry_server"
)

var primeDbCmd = &cobra.Command{
	Use:   "prime-db [username]",
	Short: "Prime the database with role and user",
	Long: `Prime the database by creating the required role and user.

This command:
- Creates the role 'toolhive_registry_server' if it doesn't exist
- Creates a user (specified as positional argument) if it doesn't exist
- Grants the role to the user
- Reads the password from STDIN

The command uses the --config option to connect to the database.`,
	Args: cobra.ExactArgs(1),
	RunE: runPrimeDb,
}

func init() {
	primeDbCmd.Flags().String("config", "", "Path to configuration file (YAML format, required)")
	primeDbCmd.Flags().Bool("dry-run", false, "Print the SQL that would be executed to standard output")

	err := viper.BindPFlag("config", primeDbCmd.Flags().Lookup("config"))
	if err != nil {
		logger.Fatalf("Failed to bind config flag: %v", err)
	}

	// Mark config as required
	if err := primeDbCmd.MarkFlagRequired("config"); err != nil {
		logger.Fatalf("Failed to mark config flag as required: %v", err)
	}
}

func runPrimeDb(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	username := args[0]
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return fmt.Errorf("failed to get dry-run flag: %w", err)
	}
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("failed to get config flag: %w", err)
	}

	var reader io.Reader
	if term.IsTerminal(int(os.Stdin.Fd())) {
		logger.Infof("Reading password from terminal...")
		passwordReader, err := readerFromTerminal()
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		reader = passwordReader
	} else {
		reader = cmd.InOrStdin()
	}

	passwordBytes, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	password := sanitizePassword(string(passwordBytes))

	// Load and execute the template
	primeSQL, err := executePrimeTemplate(username, password)
	if err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	if dryRun {
		fmt.Println(primeSQL)
		return nil
	}

	err = executePrimeSQL(ctx, primeSQL, configPath)
	if err != nil {
		return fmt.Errorf("failed to execute prime SQL: %w", err)
	}

	return nil
}

func executePrimeSQL(ctx context.Context, primeSQL string, configPath string) error {
	cfg, err := config.LoadConfig(config.WithConfigPath(configPath))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Database == nil {
		return fmt.Errorf("database configuration is required")
	}

	connString := cfg.Database.GetMigrationConnectionString()

	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(ctx); closeErr != nil {
			logger.Errorf("Error closing database connection: %v", closeErr)
		}
	}()

	tx, err := conn.BeginTx(
		ctx,
		pgx.TxOptions{
			IsoLevel:   pgx.Serializable,
			AccessMode: pgx.ReadWrite,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
			logger.Errorf("failed to rollback transaction: %v", err)
		}
	}()

	if _, err := tx.Exec(ctx, primeSQL); err != nil {
		return fmt.Errorf("failed to prime database: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Infof("Database primed successfully", fixedRoleName)
	return nil
}

// executePrimeTemplate loads and executes the prime.sql.tmpl template
func executePrimeTemplate(username, password string) (string, error) {
	templateData, err := database.GetPrimeTemplate()
	if err != nil {
		return "", fmt.Errorf("failed to get template: %w", err)
	}

	tmpl, err := template.New("prime").Parse(string(templateData))
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	data := struct {
		Username string
		Password string
	}{
		Username: username,
		Password: password,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

func readerFromTerminal() (io.Reader, error) {
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return nil, fmt.Errorf("failed to read password: %w", err)
	}
	if len(passwordBytes) == 0 {
		return nil, fmt.Errorf("password cannot be empty")
	}

	return bytes.NewReader(passwordBytes), nil
}

func sanitizePassword(password string) string {
	password = strings.TrimSpace(password)
	password = strings.ReplaceAll(password, "'", "''")
	return password
}
