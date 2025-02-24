package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/codedeploy"
)

type Event struct{}

func handler(ctx context.Context, event Event) (string, error) {
	// Load AWS SDK Config
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load AWS config: %v", err)
	}

	// Create CodeDeploy client
	client := codedeploy.NewFromConfig(cfg)

	// Get application and deployment group names from environment variables
	applicationName := os.Getenv("APPLICATION_NAME")
	deploymentGroupName := os.Getenv("DEPLOYMENT_GROUP_NAME")

	if applicationName == "" || deploymentGroupName == "" {
		return "", fmt.Errorf("missing required environment variables")
	}

	// Create deployment request
	deployInput := &codedeploy.CreateDeploymentInput{
		ApplicationName:     aws.String(applicationName),
		DeploymentGroupName: aws.String(deploymentGroupName),
	}

	// Call CodeDeploy
	resp, err := client.CreateDeployment(ctx, deployInput)
	if err != nil {
		return "", fmt.Errorf("failed to create deployment: %v", err)
	}

	return fmt.Sprintf("Deployment started: %s", *resp.DeploymentId), nil
}

func main() {
	lambda.Start(handler)
}
