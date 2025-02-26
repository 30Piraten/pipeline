package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/codedeploy"
	"github.com/aws/aws-sdk-go-v2/service/codedeploy/types"
	"github.com/aws/aws-sdk-go-v2/service/codepipeline"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Global AWS clients
var (
	codeDeployClient     *codedeploy.Client
	codePipelineClient   *codepipeline.Client
	secretsManagerClient *secretsmanager.Client
)

// This init() function will run once Lambda starts
func init() {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		panic(fmt.Sprintf("failed to load AWS config: %v", err))
	}

	// Here, we initialize AWS clients once
	codeDeployClient = codedeploy.NewFromConfig(cfg)
	codePipelineClient = codepipeline.NewFromConfig(cfg)
	secretsManagerClient = secretsmanager.NewFromConfig(cfg)
}

// CodePipelineEvent is the structure of the event received from CodePipeline
type CodePipelineEvent struct {
	CodePipelineJob struct {
		ID   string  `json:"id"`
		Data JobData `json:"data"`
	} `json:"CodePipeline.job"`
}

type JobData struct {
	InputArtifacts  []Artifact `json:"inputArtifacts"`
	OutputArtifacts []Artifact `json:"outputArtifacts"`
}

type Artifact struct {
	Location Location `json:"location"`
	Name     string   `json:"name"`
	Revision string   `json:"revision"`
}

type Location struct {
	S3Location S3Location `json:"s3Location"`
	Type       string     `json:"type"`
}

type S3Location struct {
	BucketName string `json:"bucketName"`
	ObjectKey  string `json:"objectKey"`
}

// Defined a maximum time to wait for deployment to complete (in seconds)
// Depending on the results, might increase or decrease or leave constant!
const maxWaitTimeSeconds = 300 // 5 minutes

// We retrieve the getGitHubToken from AWS Secrets Manager
func getGitHubToken(ctx context.Context) (string, error) {
	secretARN := os.Getenv("GITHUB_TOKEN")
	if secretARN == "" {
		return "", fmt.Errorf("GITHUB_TOKEN environment variable not set")
	}

	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretARN),
	}

	result, err := secretsManagerClient.GetSecretValue(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to get secret value: %v", err)
	}

	return *result.SecretString, nil
}

// Here, monitorDeployment waits for the deployment to reach a terminal state
// This state could be (failed, succeeded or stopped)
func monitorDeployment(ctx context.Context, deploymentID string) error {
	log.Printf("Monitoring deployment status for: %s", deploymentID)
	startTime := time.Now()
	endTime := startTime.Add(time.Second * maxWaitTimeSeconds)

	for time.Now().Before(endTime) {
		input := &codedeploy.GetDeploymentInput{
			DeploymentId: aws.String(deploymentID),
		}

		result, err := codeDeployClient.GetDeployment(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to get deployment status: %v", err)
		}

		status := result.DeploymentInfo.Status
		log.Printf("Current deployment status: %s", status)

		// Check if the deployment has reached a terminal state
		switch status {
		case types.DeploymentStatusSucceeded:
			log.Printf("Deployment %s succeeded", deploymentID)
			return nil
		case types.DeploymentStatusFailed, types.DeploymentStatusStopped:
			return fmt.Errorf("deployment %s ended with status: %s", deploymentID, status)
		}

		// This takes the semblance of a loop.
		// And waiting before checking again.
		time.Sleep(15 * time.Second)
	}

	return fmt.Errorf("timed out waiting for deployment %s to complete", deploymentID)
}

func handler(ctx context.Context, event CodePipelineEvent) error {
	// We can also log the full event for debugging (testing).
	// This isn't adviced for production stage.
	// eventJSON, _ := json.MarshalIndent(event, "", "  ")
	// log.Printf("Received event: %s", eventJSON)

	// We extract the CodePipeline job ID from the event
	jobID := event.CodePipelineJob.ID
	if jobID == "" {
		log.Println("Error: Missing Job ID")
		return fmt.Errorf("job ID not found in event")
	}

	// And get the required environment variables
	applicationName := os.Getenv("APPLICATION_NAME")
	deploymentGroupName := os.Getenv("DEPLOYMENT_GROUP_NAME")

	if applicationName == "" || deploymentGroupName == "" {
		log.Printf("Missing required environment variables: APPLICATION_NAME=%s, DEPLOYMENT_GROUP_NAME=%s",
			applicationName, deploymentGroupName)
		reportFailure(ctx, jobID, "Missing required environment variables")
		return nil
	}

	// Here we extract the S3 artifact information
	var s3BucketName, s3ObjectKey string
	if len(event.CodePipelineJob.Data.InputArtifacts) > 0 {
		artifact := event.CodePipelineJob.Data.InputArtifacts[0]
		s3BucketName = artifact.Location.S3Location.BucketName
		s3ObjectKey = artifact.Location.S3Location.ObjectKey
		log.Printf("Using artifact from S3: bucket=%s, key=%s", s3BucketName, s3ObjectKey)
	} else {
		log.Println("Warning: No input artifacts found in the CodePipeline event")
	}

	// And try to get the GitHub token (for potential future use)
	// But continue anyway, as we might not need it for this particular deployment
	_, err := getGitHubToken(ctx)
	if err != nil {
		log.Printf("Warning: Failed to get GitHub token: %v", err)
	}

	// Create deployment request
	deployInput := &codedeploy.CreateDeploymentInput{
		ApplicationName:     aws.String(applicationName),
		DeploymentGroupName: aws.String(deploymentGroupName),
		Description:         aws.String(fmt.Sprintf("Deployment triggered by CodePipeline job %s", jobID)),
	}

	// We add an S3 revision if we have valid artifact information
	if s3BucketName != "" && s3ObjectKey != "" {
		deployInput.Revision = &types.RevisionLocation{
			RevisionType: types.RevisionLocationTypeS3,
			S3Location: &types.S3Location{
				Bucket:     aws.String(s3BucketName),
				Key:        aws.String(s3ObjectKey),
				BundleType: types.BundleTypeZip,
			},
		}
	} else {
		log.Println("Warning: No S3 location available for deployment, continuing without revision specification")
	}

	// We create the deployment
	resp, err := codeDeployClient.CreateDeployment(ctx, deployInput)
	if err != nil {
		log.Printf("Failed to create deployment: %v", err)
		reportFailure(ctx, jobID, fmt.Sprintf("Failed to create deployment: %v", err))
		return nil
	}

	deploymentID := *resp.DeploymentId
	log.Printf("Successfully created deployment: %s", deploymentID)

	// And monitor the deployment until completion or timeout
	err = monitorDeployment(ctx, deploymentID)
	if err != nil {
		log.Printf("Deployment monitoring failed: %v", err)
		reportFailure(ctx, jobID, fmt.Sprintf("Deployment monitoring failed: %v", err))
		return nil
	}

	return reportSuccess(ctx, jobID)
}

// We notify CodePipeline of success
func reportSuccess(ctx context.Context, jobID string) error {
	log.Printf("Reporting success for job: %s", jobID)
	_, err := codePipelineClient.PutJobSuccessResult(ctx, &codepipeline.PutJobSuccessResultInput{
		JobId: aws.String(jobID),
	})
	if err != nil {
		log.Printf("Failed to report success to CodePipeline: %v", err)
		return fmt.Errorf("failed to report success to CodePipeline: %v", err)
	}
	log.Printf("Successfully reported job completion to CodePipeline")
	return nil
}

// As well as notify CodePipeline of failure
func reportFailure(ctx context.Context, jobID string, message string) {
	log.Printf("Reporting failure for job %s: %s", jobID, message)
	_, err := codePipelineClient.PutJobFailureResult(ctx, &codepipeline.PutJobFailureResultInput{
		JobId: aws.String(jobID),
	})
	if err != nil {
		log.Printf("Failed to report failure to CodePipeline: %v", err)
		log.Printf("Failed to report failure to CodePipeline: %v", err)
	}
	log.Printf("Successfully reported job failure to CodePipeline")
}

// The handler() function is called here
func main() {
	lambda.Start(handler)
}
