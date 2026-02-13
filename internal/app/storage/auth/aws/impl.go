// Package aws implements dynamic authentication for AWS RDS IAM.
package aws

import (
	"context"
	"fmt"
	"net/http"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

const (
	awsRegionDetect = "detect"
)

// getRegion resolves the AWS region from the configuration, detecting
// it from IMDS if the region is set to "detect".
func getRegion(ctx context.Context, cfg *config.DatabaseConfig) (string, error) {
	if cfg.DynamicAuth.AWSRDSIAM.Region == "" {
		return "", fmt.Errorf("AWS RDS IAM region is not configured")
	}

	if cfg.DynamicAuth.AWSRDSIAM.Region == awsRegionDetect {
		imdsClient := imds.New(imds.Options{
			HTTPClient: &http.Client{
				Timeout: 2 * time.Second,
			},
		})

		regionOut, err := imdsClient.GetRegion(ctx, &imds.GetRegionInput{})
		if err != nil {
			return "", fmt.Errorf("failed to get region from IMDS: %w", err)
		}

		return regionOut.Region, nil
	}

	return cfg.DynamicAuth.AWSRDSIAM.Region, nil
}

// getToken generates an AWS RDS IAM authentication token for the
// given user. The token can be used as a password in a PostgreSQL connection string.
func getToken(ctx context.Context, cfg *config.DatabaseConfig, region, user string) (string, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return "", fmt.Errorf("failed to load AWS config: %w", err)
	}

	dbEndpoint := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	token, err := auth.BuildAuthToken(ctx, dbEndpoint, region, user, awsCfg.Credentials)
	if err != nil {
		return "", fmt.Errorf("failed to build authentication token: %w", err)
	}

	return token, nil
}

// NewToken creates a function that resolves an AWS RDS IAM authentication token.
func NewToken(
	ctx context.Context,
	cfg *config.DatabaseConfig,
	user string,
) (string, error) {
	region, err := getRegion(ctx, cfg)
	if err != nil {
		return "", err
	}

	return getToken(ctx, cfg, region, user)
}

// PgxAuthFunc creates a function that authenticates with AWS RDS IAM.
//
// It assumes that the role attached to the workload can be used to
// authenticate with the database. If the region is "detect", it will try to
// automatically detect it from the instance metadata via IMDS.
func PgxAuthFunc(
	ctx context.Context,
	cfg *config.DatabaseConfig,
	user string,
) (func(ctx context.Context, connConfig *pgx.ConnConfig) error, error) {
	region, err := getRegion(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, connConfig *pgx.ConnConfig) error {
		token, err := getToken(ctx, cfg, region, user)
		if err != nil {
			return err
		}

		connConfig.Password = token
		return nil
	}, nil
}
