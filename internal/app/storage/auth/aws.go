package auth

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

// awsRdsIamAuthFunc creates a function that authenticates with AWS RDS IAM.
//
// It assumes that the role attached to the workload can be used to
// authenticate with the database. If the region is "detect", it will try to
// automatically detect it from the instance metadata via IMDS.
func awsRdsIamAuthFunc(
	ctx context.Context,
	cfg *config.DatabaseConfig,
) (func(ctx context.Context, connConfig *pgx.ConnConfig) error, error) {
	if cfg.DynamicAuth.AWSRDSIAM.Region == "" {
		return nil, fmt.Errorf("AWS RDS IAM region is not configured")
	}

	var region string
	if cfg.DynamicAuth.AWSRDSIAM.Region == awsRegionDetect {
		imdsClient := imds.New(imds.Options{
			HTTPClient: &http.Client{
				Timeout: 2 * time.Second,
			},
		})

		regionOut, err := imdsClient.GetRegion(ctx, &imds.GetRegionInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to get region from IMDS: %w", err)
		}

		region = regionOut.Region
	} else {
		region = cfg.DynamicAuth.AWSRDSIAM.Region
	}

	return func(ctx context.Context, connConfig *pgx.ConnConfig) error {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return fmt.Errorf("failed to load AWS config: %w", err)
		}

		dbEndpoint := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
		token, err := auth.BuildAuthToken(ctx, dbEndpoint, region, cfg.User, awsCfg.Credentials)
		if err != nil {
			return fmt.Errorf("failed to build authentication token: %w", err)
		}

		connConfig.Password = token
		return nil
	}, nil
}
