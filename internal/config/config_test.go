//nolint:tparallel // Environment variable tests cannot run in parallel
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseConfig_GetPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      *DatabaseConfig
		envValue    string
		fileContent string
		want        string
		wantErr     bool
	}{
		{
			name: "password from file",
			config: &DatabaseConfig{
				PasswordFile: "testdata/password.txt",
			},
			fileContent: "password-from-file\n",
			want:        "password-from-file",
			wantErr:     false,
		},
		{
			name: "password from file with whitespace",
			config: &DatabaseConfig{
				PasswordFile: "testdata/password-whitespace.txt",
			},
			fileContent: "  password-with-spaces  \n\t",
			want:        "password-with-spaces",
			wantErr:     false,
		},
		{
			name:     "password from environment variable",
			config:   &DatabaseConfig{},
			envValue: "env-password",
			want:     "env-password",
			wantErr:  false,
		},
		{
			name: "priority: file over env",
			config: &DatabaseConfig{
				PasswordFile: "testdata/password.txt",
			},
			envValue:    "env-password",
			fileContent: "file-password",
			want:        "file-password",
			wantErr:     false,
		},
		{
			name:    "no password configured",
			config:  &DatabaseConfig{},
			want:    "",
			wantErr: true,
		},
		{
			name: "file does not exist",
			config: &DatabaseConfig{
				PasswordFile: "testdata/nonexistent.txt",
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			// Don't run in parallel if we're setting environment variables
			// to avoid race conditions
			if tt.envValue == "" {
				t.Parallel()
			}

			// Setup: Create temporary file if fileContent is provided
			if tt.fileContent != "" {
				tmpDir := t.TempDir()
				tmpFile := filepath.Join(tmpDir, "password.txt")
				err := os.WriteFile(tmpFile, []byte(tt.fileContent), 0600)
				if err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
				tt.config.PasswordFile = tmpFile
			}

			// Setup: Set environment variable if provided
			if tt.envValue != "" {
				oldEnv := os.Getenv("THV_DATABASE_PASSWORD")
				os.Setenv("THV_DATABASE_PASSWORD", tt.envValue)
				defer os.Setenv("THV_DATABASE_PASSWORD", oldEnv)
			}

			// Execute
			got, err := tt.config.GetPassword()

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("GetPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetPassword() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDatabaseConfig_GetConnectionString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      *DatabaseConfig
		envPassword string
		want        string
		wantErr     bool
	}{
		{
			name: "basic connection string with env var",
			config: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
			},
			envPassword: "testpass",
			want:        "postgres://testuser:testpass@localhost:5432/testdb?sslmode=require",
			wantErr:     false,
		},
		{
			name: "connection string with special characters in password",
			config: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
				SSLMode:  "disable",
			},
			envPassword: "p@ssw0rd!#$%",
			want:        "postgres://testuser:p%40ssw0rd%21%23%24%25@localhost:5432/testdb?sslmode=disable",
			wantErr:     false,
		},
		{
			name: "connection string with custom ssl mode",
			config: &DatabaseConfig{
				Host:     "postgres.example.com",
				Port:     5432,
				User:     "admin",
				Database: "proddb",
				SSLMode:  "verify-full",
			},
			envPassword: "secret",
			want:        "postgres://admin:secret@postgres.example.com:5432/proddb?sslmode=verify-full",
			wantErr:     false,
		},
		{
			name: "no password configured",
			config: &DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Database: "testdb",
			},
			envPassword: "",
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			// Don't run in parallel when setting env vars
			if tt.envPassword == "" {
				t.Parallel()
			}

			// Setup environment variable
			if tt.envPassword != "" {
				oldEnv := os.Getenv("THV_DATABASE_PASSWORD")
				os.Setenv("THV_DATABASE_PASSWORD", tt.envPassword)
				defer os.Setenv("THV_DATABASE_PASSWORD", oldEnv)
			}

			got, err := tt.config.GetConnectionString()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetConnectionString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetConnectionString() = %v, want %v", got, tt.want)
			}
		})
	}
}
