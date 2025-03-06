package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/codedeploy"
	"github.com/aws/aws-sdk-go-v2/service/codedeploy/types"
	"github.com/aws/aws-sdk-go-v2/service/codepipeline"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Global AWS clients
var (
	codeDeployClient     *codedeploy.Client
	codePipelineClient   *codepipeline.Client
	secretsManagerClient *secretsmanager.Client
	s3Client             *s3.Client
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
	s3Client = s3.NewFromConfig(cfg)

	log.Printf("Lambda initialization completed")
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

func getMaxWaitTime() int {
	maxWaitTimeString := os.Getenv("MAX_DEPLOYMENT_WAIT_TIME")
	if maxWaitTimeString != "" {
		maxWaitTime, err := strconv.Atoi(maxWaitTimeString)
		if err == nil && maxWaitTime > 0 {
			return maxWaitTime
		}
		log.Printf("Warning: Invalid MAX_DEPLOYMENT_WAIT_TIME value %s, using default", maxWaitTimeString)
	}
	return 600
}

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

// monitorDeployment waits for the deployment to reach a terminal state
// This state could be (failed, succeeded or stopped)
func monitorDeployment(ctx context.Context, deploymentID string) error {
	log.Printf("Monitoring deployment status for: %s", deploymentID)
	startTime := time.Now()
	maxWaitTime := getMaxWaitTime()
	endTime := startTime.Add(time.Second * time.Duration(maxWaitTime))

	// Initial wait time for exponential backoff
	waitTime := 2 * time.Second
	backoffMaxWaitTime := 30 * time.Second

	attempt := 1

	for time.Now().Before(endTime) {
		input := &codedeploy.GetDeploymentInput{
			DeploymentId: aws.String(deploymentID),
		}

		log.Printf("Checking deployment status (attempt %d): %s", attempt, deploymentID)
		result, err := codeDeployClient.GetDeployment(ctx, input)
		if err != nil {

			// Dont fail immediately on API errors, retry with backoff
			if attempt < 3 {
				time.Sleep(waitTime)
				attempt++
				continue
			}
			return fmt.Errorf("failed to get deployment status after %d attempts: %v", attempt, err)
		}

		status := result.DeploymentInfo.Status
		log.Printf("Current deployment status: %s", status)

		// Check if the deployment has reached a terminal state
		switch status {
		case types.DeploymentStatusSucceeded:
			log.Printf("Deployment %s succeeded", deploymentID)
			return nil
		case types.DeploymentStatusFailed:
			errInfo := "No error information available"
			if result.DeploymentInfo.ErrorInformation != nil && result.DeploymentInfo.ErrorInformation.Message != nil {
				errInfo = *result.DeploymentInfo.ErrorInformation.Message
			}
			return fmt.Errorf("deployment %s failed: %s", deploymentID, errInfo)
		case types.DeploymentStatusStopped:
			return fmt.Errorf("deployment %s stopped with status: %s", deploymentID, status)
		}

		// Use exponential backoff for the next attempt
		waitTime = time.Duration(math.Min(float64(waitTime*2), float64(backoffMaxWaitTime)))
		log.Printf("Waiting %v before next status check", waitTime)
		time.Sleep(waitTime)
		attempt++
	}

	return fmt.Errorf("timed out waiting for deployment %s to complete", deploymentID)
}

// runPreDeploymentValidation performs validation checks before deployment
func runPreDeploymentValidation(ctx context.Context, applicationName, deploymentGroupName, s3BucketName, s3ObjectKey string) error {
	log.Printf("Running pre-deployment validation for %s/%s", applicationName, deploymentGroupName)

	// 1. Validate application and deployment group exist
	_, err := codeDeployClient.GetDeploymentGroup(ctx, &codedeploy.GetDeploymentGroupInput{
		ApplicationName:     aws.String(applicationName),
		DeploymentGroupName: aws.String(deploymentGroupName),
	})
	if err != nil {
		return fmt.Errorf("deployment group validation failed: %v", err)
	}

	// 2. Validate S3 artifact exists and is accessible
	if s3BucketName != "" && s3ObjectKey != "" {
		_, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(s3BucketName),
			Key:    aws.String(s3ObjectKey),
		})
		if err != nil {
			return fmt.Errorf("artifact validation failed: %v", err)
		}
	}

	// 3. Check if there's already an in-progress deployment for this group
	listDeploymentsInput := &codedeploy.ListDeploymentsInput{
		ApplicationName:     aws.String(applicationName),
		DeploymentGroupName: aws.String(deploymentGroupName),
		IncludeOnlyStatuses: []types.DeploymentStatus{
			types.DeploymentStatusCreated,
			types.DeploymentStatusQueued,
			types.DeploymentStatusInProgress,
		},
	}

	listResult, err := codeDeployClient.ListDeployments(ctx, listDeploymentsInput)
	if err != nil {
		log.Printf("Warning: Could not check for in-progress deployments: %v", err)
	} else if len(listResult.Deployments) > 0 {
		log.Printf("Warning: There are %d in-progress deployments for this group", len(listResult.Deployments))
	}

	// 4. Validate any custom pre-deployment requirements
	// This could include checking infrastructure readiness, database status, etc.
	// For example:
	err = validateRequiredInfrastructure(ctx, applicationName)
	if err != nil {
		return fmt.Errorf("infrastructure validation failed: %v", err)
	}

	log.Printf("Pre-deployment validation completed successfully")
	return nil
}

// validateRequiredInfrastructure checks if required infrastructure is available
func validateRequiredInfrastructure(ctx context.Context, applicationName string) error {
	// Get the health check URL from environment variables
	healthCheckURL := os.Getenv("HEALTH_CHECK_URL")
	if healthCheckURL == "" {
		log.Printf("No health check URL configured, skipping infrastructure validation")
		return nil
	}

	// Perform a simple health check
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", healthCheckURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health check returned non-success status code: %d", resp.StatusCode)
	}

	log.Printf("Infrastructure validation succeeded: health check returned %d", resp.StatusCode)
	return nil
}

// runPostDeploymentValidation performs validation checks after deployment
func runPostDeploymentValidation(ctx context.Context, applicationName, deploymentGroupName, deploymentID string) error {
	log.Printf("Running post-deployment validation for deployment: %s", deploymentID)

	// 1. Get deployment information to find deployment targets
	deploymentInfo, err := codeDeployClient.GetDeployment(ctx, &codedeploy.GetDeploymentInput{
		DeploymentId: aws.String(deploymentID),
	})
	if err != nil {
		return fmt.Errorf("failed to get deployment info: %v", err)
	}

	// 2. Verify deployment succeeded on all targets
	targetsInput := &codedeploy.ListDeploymentTargetsInput{
		DeploymentId: aws.String(deploymentID),
	}

	targetsResult, err := codeDeployClient.ListDeploymentTargets(ctx, targetsInput)
	if err != nil {
		return fmt.Errorf("failed to list deployment targets: %v", err)
	}

	// 3. Check each target's status
	for _, targetId := range targetsResult.TargetIds {
		targetInfo, err := codeDeployClient.GetDeploymentTarget(ctx, &codedeploy.GetDeploymentTargetInput{
			DeploymentId: aws.String(deploymentID),
			TargetId:     aws.String(targetId),
		})
		if err != nil {
			return fmt.Errorf("failed to get target info for %s: %v", targetId, err)
		}

		// Check if this target succeeded
		if targetInfo.DeploymentTarget.InstanceTarget != nil &&
			targetInfo.DeploymentTarget.InstanceTarget.Status != "Succeeded" {
			return fmt.Errorf("deployment failed on target %s with status: %s",
				targetId, targetInfo.DeploymentTarget.InstanceTarget.Status)
		}
	}

	// 4. Perform application-specific health checks
	err = validateApplicationHealth(ctx)
	if err != nil {
		return fmt.Errorf("application health validation failed: %v", err)
	}

	log.Printf("Post-deployment validation completed successfully")

	log.Printf("Deployment %s succeeded", deploymentID)
	log.Printf("DeploymentInfo %v", deploymentInfo)

	return nil
}

// validateApplicationHealth checks if the application is healthy after deployment
func validateApplicationHealth(ctx context.Context) error {
	// Get the application health check URL from environment variables
	appHealthCheckURL := os.Getenv("APP_HEALTH_CHECK_URL")
	if appHealthCheckURL == "" {
		log.Printf("No application health check URL configured, skipping application health validation")
		return nil
	}

	// Number of retries for health check
	maxRetries := 3

	// Wait time between retries
	retryWaitTime := 5 * time.Second

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	var lastErr error

	// Retry the health check a few times to allow the application to start up
	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequestWithContext(ctx, "GET", appHealthCheckURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create health check request: %v", err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("health check failed (attempt %d): %v", i+1, err)
			log.Printf("%v, retrying in %v", lastErr, retryWaitTime)
			time.Sleep(retryWaitTime)
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("health check returned non-success status code (attempt %d): %d", i+1, resp.StatusCode)
			log.Printf("%v, retrying in %v", lastErr, retryWaitTime)
			time.Sleep(retryWaitTime)
			continue
		}

		// If we got here, the health check succeeded
		log.Printf("Application health check succeeded: status code %d", resp.StatusCode)
		return nil
	}

	// If we got here, all retries failed
	return fmt.Errorf("application health validation failed after %d attempts: %v", maxRetries, lastErr)
}

func handler(ctx context.Context, event CodePipelineEvent) error {
	// Logging sanitized version of the event for debugging
	sanitizedEventVersion := event
	if len(sanitizedEventVersion.CodePipelineJob.Data.InputArtifacts) > 0 {
		for i := range sanitizedEventVersion.CodePipelineJob.Data.InputArtifacts {
			if len(sanitizedEventVersion.CodePipelineJob.Data.InputArtifacts[i].Revision) > 10 {
				sanitizedEventVersion.CodePipelineJob.Data.InputArtifacts[i].Revision = sanitizedEventVersion.CodePipelineJob.Data.InputArtifacts[i].Revision[:10] + "..."
			}
		}
	}

	eventJSON, _ := json.MarshalIndent(event, "", "  ")
	log.Printf("Received event: %s", eventJSON)

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
		err := fmt.Errorf("missing required environment variables: APPLICATION_NAME=%s, DEPLOYMENT_GROUP_NAME=%s",
			applicationName, deploymentGroupName)
		log.Printf("%v", err)
		reportFailure(ctx, jobID, "Missing required environment variables")
		return err
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

	// Run pre-deployment validation
	err = runPreDeploymentValidation(ctx, applicationName, deploymentGroupName, s3BucketName, s3ObjectKey)
	if err != nil {
		log.Printf("Pre-deployment validation failed: %v", err)
		reportFailure(ctx, jobID, fmt.Sprintf("Pre-deployment validation failed: %v", err))
		return err
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

	// We create the deployment with retry logic
	var deploymentID string
	for attempt := 1; attempt <= 3; attempt++ {
		log.Printf("Creating deployment (attempt %d): %s", attempt, jobID)
		resp, err := codeDeployClient.CreateDeployment(ctx, deployInput)
		if err != nil {
			log.Printf("Failed to create deployment: %v", err)
			if attempt == 3 {
				reportFailureErr := fmt.Errorf("failed to create deployment after %d attempts: %v", attempt, err)
				reportFailure(ctx, jobID, fmt.Sprintf("Failed to create deployment after %d attempts: %v", attempt, err))
				return reportFailureErr
			}
			time.Sleep(time.Duration(math.Pow(2, float64(attempt))) * time.Second)
			continue
		}
		deploymentID = *resp.DeploymentId
		log.Printf("Successfully created deployment: %s", deploymentID)
		break
	}

	// If we failed to create a deployment, report failure
	if deploymentID == "" {
		err := fmt.Errorf("failed to create deployment")
		reportFailure(ctx, jobID, "Failed to create deployment")
		return err
	}

	// Monitor the deployment until completion or timeout
	err = monitorDeployment(ctx, deploymentID)
	if err != nil {
		log.Printf("Deployment monitoring failed: %v", err)
		reportFailure(ctx, jobID, fmt.Sprintf("Deployment monitoring failed: %v", err))
		return err
	}

	// Run post-deployment validation
	err = runPostDeploymentValidation(ctx, applicationName, deploymentGroupName, deploymentID)
	if err != nil {
		log.Printf("Post-deployment validation failed: %v", err)
		reportFailure(ctx, jobID, fmt.Sprintf("Post-deployment validation failed: %v", err))
		return err
	}

	// The deployment is successful if we make it here
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
	}
	log.Printf("Successfully reported job failure to CodePipeline")
}

// The handler() function is called here
func main() {
	lambda.Start(handler)
}
