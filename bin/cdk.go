package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatchactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodebuild"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodedeploy"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipeline"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipelineactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3assets"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssns"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssqs"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

type PipelineBuildV1Props struct {
	awscdk.StackProps
}

// Validate env variables
func checkEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("WARNING: %s environment variable is required!", key)
	}

	return value
}

// NewPipelineBuildV1 creates a new CDK stack that sets up the CI/CD pipeline
func NewPipelineBuildV1(scope constructs.Construct, id string, props *PipelineBuildV1Props) awscdk.Stack {
	// Stack initialization with props
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Configure the stack synthesizer with custom qualifier
	synth := awscdk.NewDefaultStackSynthesizer(&awscdk.DefaultStackSynthesizerProps{
		Qualifier: jsii.String("pipeline-artifact-bucket-v1"),
	})

	sprops.Synthesizer = synth

	// Retrieve the GitHub token from Secrets Manager
	githubSecret := awssecretsmanager.Secret_FromSecretNameV2(stack, jsii.String("GitHubTokenSecret"), jsii.String("token"))
	oauthTokenSecret := githubSecret.SecretValue()

	// Create Dead Letter Queue for Lambda function
	deadLetterQueue := awssqs.NewQueue(stack, jsii.String("LambdaDLQ"), &awssqs.QueueProps{
		QueueName:       jsii.String("lambda-deploy-dlq"),
		RetentionPeriod: awscdk.Duration_Days(jsii.Number(7)),
	})

	// Get the Lambda function directory path
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("Could not get file name")
	}
	lambdaDir := filepath.Join(filepath.Dir(filename), "lambda")

	// Create the Lambda function with configuration
	lambdaFunctionV1 := awslambda.NewFunction(stack, jsii.String("pipelineHandler"), &awslambda.FunctionProps{
		Runtime:         awslambda.Runtime_PROVIDED_AL2(),
		Handler:         jsii.String("bootstrap"),
		RetryAttempts:   jsii.Number(2),
		MemorySize:      jsii.Number(1024),
		Timeout:         awscdk.Duration_Minutes(jsii.Number(6)),
		Architecture:    awslambda.Architecture_X86_64(),
		DeadLetterQueue: deadLetterQueue,
		CurrentVersionOptions: &awslambda.VersionOptions{
			RemovalPolicy: awscdk.RemovalPolicy_RETAIN,
			Description:   jsii.String("Automated Version"),
		},
		Code: awslambda.Code_FromAsset(jsii.String(lambdaDir), &awss3assets.AssetOptions{}),
		Environment: &map[string]*string{
			"GITHUB_TOKEN":             githubSecret.SecretArn(),
			"APPLICATION_NAME":         jsii.String("LambdaDeployApp"),
			"DEPLOYMENT_GROUP_NAME":    jsii.String("LambdaDeploymentGroup"),
			"MAX_DEPLOYMENT_WAIT_TIME": jsii.String("600"), // 6 minutes in seconds
			// "HEALTH_CHECK_URL":         TODO,
			// "APP_HEALTH_CHECK_URL":     TODO,
		},
		Tracing: awslambda.Tracing_ACTIVE,
	})

	// Create the Lambda alias for deployment
	lambdaAlias := awslambda.NewAlias(stack, jsii.String("testing"), &awslambda.AliasProps{
		AliasName:   jsii.String("Live"),
		Description: jsii.String("Lambda Alias"),
		Version:     lambdaFunctionV1.CurrentVersion(),
	})

	// Define CloudWatch alarm for Lambda errors
	lambdaErrorsAlarm := awscloudwatch.NewAlarm(stack, jsii.String("LambdaErrorsAlarm"), &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Alarm for Lambda errors"),
		AlarmName:        jsii.String("LambdaErrorsAlarm"),
		Metric: awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
			Namespace:  jsii.String("AWS/Lambda"),
			MetricName: jsii.String("Errors"),
			Statistic:  jsii.String("Sum"),
			Period:     awscdk.Duration_Minutes(jsii.Number(1)),
			DimensionsMap: &map[string]*string{
				"FunctionName": lambdaFunctionV1.FunctionName(),
			},
		}),
		EvaluationPeriods:  jsii.Number(1),
		Threshold:          jsii.Number(1),
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD,
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,
	})

	// Set up the CodeDeploy application for Lambda
	codeDeployV1 := awscodedeploy.NewLambdaApplication(stack, jsii.String("LambdaDeployV1"), &awscodedeploy.LambdaApplicationProps{
		ApplicationName: jsii.String(checkEnv("CODE_DEPLOY_APP_NAME")),
	})

	// Here, we configure the deployment group with canary deployment and health checks
	deploymentGroupV1 := awscodedeploy.NewLambdaDeploymentGroup(stack, jsii.String("BGCDeployment"), &awscodedeploy.LambdaDeploymentGroupProps{
		Application:      codeDeployV1,
		Alias:            lambdaAlias,
		DeploymentConfig: awscodedeploy.LambdaDeploymentConfig_CANARY_10PERCENT_5MINUTES(),
		AutoRollback: &awscodedeploy.AutoRollbackConfig{
			FailedDeployment:  jsii.Bool(true),
			StoppedDeployment: jsii.Bool(true),
			DeploymentInAlarm: jsii.Bool(true),
		},
		Alarms: &[]awscloudwatch.IAlarm{lambdaErrorsAlarm},
	})

	// Lambda IAM role definition
	lambdaRoleV1 := awsiam.NewRole(stack, jsii.String("LambdaRoleV1"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("lambda.amazonaws.com"), &awsiam.ServicePrincipalOpts{}),
	})

	// Granting Lambda function permissions to access GitHub secret
	lambdaRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
		Resources: jsii.Strings(*githubSecret.SecretArn()),
	}))

	// Granting Lambda function permissions for CodeDeploy operations
	lambdaRoleV1.AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"codedeploy:CreateDeployment",
			"codedeploy:GetDeploymentConfig",
			"codedeploy:ApplicationRevision",
			"codedeploy:GetDeployment",
			"codedeploy:UpdateDeployment",
		),
		Resources: jsii.Strings(
			*deploymentGroupV1.DeploymentGroupArn(),
			*codeDeployV1.ApplicationArn(),
			fmt.Sprintf("arn:aws:codedeploy:%s:%s:deploymentconfig:*",
				*stack.Region(), *stack.Account()),
		),
	}))

	// Limit CodePipeline job result permissions to the specific pipeline
	lambdaRoleV1.AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"codepipeline:PutJobSuccessResult",
			"codepipeline:PutJobFailureResult",
		),
		Resources: jsii.Strings(fmt.Sprintf("arn:aws:codepipeline:%s:%s:*", *stack.Region(), *stack.Account())),
	}))

	// Allow CloudWatch logs with specific resource pattern
	lambdaRoleV1.AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"logs:CreateLogGroup",
			"logs:CreateLogStream",
			"logs:PutLogEvents",
		),
		Resources: jsii.Strings(
			fmt.Sprintf("arn:aws:logs:%s:%s:log-group:/aws/lambda/%s:*",
				*stack.Region(), *stack.Account(), *lambdaFunctionV1.FunctionName()),
		),
	}))

	// We allow CodePipeline to invoke Lambda
	lambdaAlias.GrantInvoke(awsiam.NewServicePrincipal(jsii.String("codepipeline.amazonaws.com"), nil))

	// And create the IAM role for CodeBuild
	codeBuildRoleV1 := awsiam.NewRole(stack, jsii.String("CodeBuildRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codebuild.amazonaws.com"), nil),
	})

	// We set up the CodeBuild project configuration
	codeBuildV1 := awscodebuild.NewProject(stack, jsii.String("CodeBuildV1"), &awscodebuild.ProjectProps{
		Source: awscodebuild.Source_GitHub(&awscodebuild.GitHubSourceProps{
			Owner: jsii.String(os.Getenv("GITHUB_OWNER")),
			Repo:  jsii.String(os.Getenv("GITHUB_REPO")),
		}),
		BuildSpec:   awscodebuild.BuildSpec_FromSourceFilename(jsii.String("cdk/buildspec.yml")),
		Role:        codeBuildRoleV1,
		ProjectName: jsii.String("CodeBuildProjectV1"),
		Environment: &awscodebuild.BuildEnvironment{
			ComputeType: awscodebuild.ComputeType_SMALL,
			BuildImage:  awscodebuild.LinuxBuildImage_AMAZON_LINUX_2_3(),
			EnvironmentVariables: &map[string]*awscodebuild.BuildEnvironmentVariable{
				"GITHUB_TOKEN": {
					Value: githubSecret.SecretArn(),
					Type:  awscodebuild.BuildEnvironmentVariableType_SECRETS_MANAGER,
				},
			},
		},
		Timeout: awscdk.Duration_Minutes(jsii.Number(15)),
	})

	// Create CloudWatch alarms for CodeBuild failures
	codeBuildFailureAlarm := awscloudwatch.NewAlarm(stack, jsii.String("CodeBuildeFailureAlamr"), &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Alert when CodeBuild project fails"),
		AlarmName:        jsii.String("CodeBuildFailureAlarm"),
		Metric: awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
			Namespace:  jsii.String("AWS/CodeBuild"),
			MetricName: jsii.String("FailedBuilds"),
			Statistic:  jsii.String("Sum"),
			Period:     awscdk.Duration_Minutes(jsii.Number(5)),
			DimensionsMap: &map[string]*string{
				"ProjectName": codeBuildV1.ProjectName(),
			},
			Unit: awscloudwatch.Unit_COUNT,
		}),
		EvaluationPeriods:  jsii.Number(1),
		Threshold:          jsii.Number(1),
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD,
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,
	})

	// Granting necessary permissions to CodeBuild role
	codeBuildRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
		Resources: jsii.Strings(*githubSecret.SecretArn()),
	}))

	codeBuildRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("codebuild:StartBuild", "codepipeline:PutJobSuccessResult"),
		Resources: jsii.Strings(*codeBuildV1.ProjectArn()),
	}))

	codeBuildRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"),
		Resources: jsii.Strings(fmt.Sprintf("arn:aws:logs:%s:%s:log-group:/aws/codebuild/%s:*", *stack.Region(), *stack.Account(), *codeBuildV1.ProjectName())),
	}))

	// Create S3 bucket for artifacts with improved security
	artifactBucketV1 := awss3.NewBucket(stack, jsii.String("ArtifactBucket"), &awss3.BucketProps{
		AutoDeleteObjects: jsii.Bool(true),
		RemovalPolicy:     awscdk.RemovalPolicy_DESTROY,
		BucketName:        jsii.String(checkEnv("S3_ARTIFACT_BUCKET_NAME")),
		Encryption:        awss3.BucketEncryption_S3_MANAGED,
		BlockPublicAccess: awss3.BlockPublicAccess_BLOCK_ALL(),
		EnforceSSL:        jsii.Bool(true),
		Versioned:         jsii.Bool(true),
	})

	// Here, we define artifacts for the pipeline stages
	sourceArtifact := awscodepipeline.NewArtifact(jsii.String("SourceArtifact"), nil)
	buildArtifact := awscodepipeline.NewArtifact(jsii.String("BuildArtifact"), nil)

	// Create IAM role for CodePipeline
	codePipelineRoleV1 := awsiam.NewRole(stack, jsii.String("CodePipelineRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codepipeline.amazonaws.com"), nil),
	})

	// Create the CodePipeline with Source, Build, and Deploy stages
	codePipelineV1 := awscodepipeline.NewPipeline(stack, jsii.String("pipelineV1"), &awscodepipeline.PipelineProps{
		PipelineName:   jsii.String("CodeBuildPipelineV1"),
		ArtifactBucket: artifactBucketV1,
		Role:           codePipelineRoleV1,
		Stages: &[]*awscodepipeline.StageProps{
			{
				StageName: jsii.String("Source"),
				Actions: &[]awscodepipeline.IAction{
					awscodepipelineactions.NewGitHubSourceAction(&awscodepipelineactions.GitHubSourceActionProps{
						ActionName: jsii.String("pipelineSource"),
						Owner:      jsii.String(checkEnv("GITHUB_OWNER")),
						Repo:       jsii.String(checkEnv("GITHUB_REPO")),
						Branch:     jsii.String(checkEnv("GITHUB_BRANCH")),
						OauthToken: oauthTokenSecret,
						Output:     sourceArtifact,
						Trigger:    awscodepipelineactions.GitHubTrigger_WEBHOOK,
					}),
				},
			},
			{
				StageName: jsii.String("Build"),
				Actions: &[]awscodepipeline.IAction{
					awscodepipelineactions.NewCodeBuildAction(&awscodepipelineactions.CodeBuildActionProps{
						ActionName: jsii.String("pipelineBuild"),
						Project:    codeBuildV1,
						Input:      sourceArtifact,
						Outputs:    &[]awscodepipeline.Artifact{buildArtifact},
					}),
				},
			},
			{
				// For the Deploy stage. The main Lambda function (lambdaFunctionV1)
				// is invoked here. This will execute the application logic within lambdaFunctionV1.
				// It is important to note, that this does not call codedeploy. The trigger lambda
				// function must be called to do this.
				// The trigger lambda function is defined in the bin/lambda/pipeline.go, which is
				// packaged with the provided.AL2 runtime.
				StageName: jsii.String("Deploy"),
				Actions: &[]awscodepipeline.IAction{
					awscodepipelineactions.NewLambdaInvokeAction(&awscodepipelineactions.LambdaInvokeActionProps{
						ActionName: jsii.String("DeployLambda"),
						Inputs:     &[]awscodepipeline.Artifact{buildArtifact},
						Lambda:     lambdaFunctionV1,
					}),
				},
			},
		},
		CrossAccountKeys: jsii.Bool(false),
	})

	// Create CloudWatch alarm for pipeline failures
	pipelineFailureAlarm := awscloudwatch.NewAlarm(stack, jsii.String("PipelineFailureAlarm"), &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Alert when CodePipeline project fails"),
		AlarmName:        jsii.String("PipelineFailureAlarm"),
		Metric: awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
			Namespace:  jsii.String("AWS/CodePipeline"),
			MetricName: jsii.String("FailedPipelines"),
			Statistic:  jsii.String("Sum"),
			Period:     awscdk.Duration_Minutes(jsii.Number(5)),
			DimensionsMap: &map[string]*string{
				"PipelineName": codePipelineV1.PipelineName(),
			},
			Unit: awscloudwatch.Unit_COUNT,
		}),
		EvaluationPeriods:  jsii.Number(1),
		Threshold:          jsii.Number(1),
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD,
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,
	})

	// Granting permissions to CodePipeline role
	artifactBucketV1.GrantReadWrite(codePipelineRoleV1, nil)
	codePipelineRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("iam:PassRole"),
		Resources: jsii.Strings(*deploymentGroupV1.Role().RoleArn()),
	}))

	codeBuildRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"codeploy:CreateDeployment",
			"codedeploy:GetDeploymentConfig",
			"codedeploy:GetDeployment",
		),
		Resources: jsii.Strings(fmt.Sprintf("arn:aws:codedeploy:%s:%s:deploymentconfig:*", *stack.Region(), *stack.Account()))}))

	codePipelineRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"codepipeline:GetPipelineExecution",
			"codepipeline:GetPipelineState",
			"codepipeline:StartPipelineExecution"),
		Resources: jsii.Strings(*codePipelineV1.PipelineArn()),
	}))

	codePipelineRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("iam:PassRole"),
		Resources: jsii.Strings(*codePipelineRoleV1.RoleArn()),
	}))

	// Here, we create CloudWatch metric for Lambda invocations
	awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
		Namespace:  jsii.String("AWS/Lambda"),
		MetricName: jsii.String("Invocations"),
		DimensionsMap: &map[string]*string{
			"FunctionName": lambdaFunctionV1.FunctionName(),
		},
		Period: awscdk.Duration_Minutes(jsii.Number(1)),
	})
	// Here, we create CloudWatch metric for Codebuild
	awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
		Namespace:  jsii.String("AWS/CodeBuild"),
		MetricName: jsii.String("BuildsSucceeded"),
		DimensionsMap: &map[string]*string{
			"ProjectName": codeBuildV1.ProjectName(),
		},
		Period: awscdk.Duration_Minutes(jsii.Number(1)),
	})

	// Add rollback metrics
	awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
		Namespace:  jsii.String("AWS/Lambda"),
		MetricName: jsii.String("AutoRollBackInitiated"),
		DimensionsMap: &map[string]*string{
			"FunctionName":    lambdaFunctionV1.FunctionName(),
			"Application":     jsii.String("codeDeployLambdaV1"),
			"DeploymentGroup": jsii.String("LambdaDeploymentGroup"),
		},
		Period: awscdk.Duration_Minutes(jsii.Number(1)),
	})

	// Create SNS topic for alarms
	alarmTopic := awssns.NewTopic(stack, jsii.String("PipelineAlarmTopic"), &awssns.TopicProps{
		TopicName:   jsii.String("pipeline-alarms"),
		DisplayName: jsii.String("Pipeline Alarms"),
	})

	// Associate alarms with pipeline
	pipelineFailureAlarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(alarmTopic))
	codeBuildFailureAlarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(alarmTopic))

	// We create the CloudFormation outputs
	awscdk.NewCfnOutput(stack, jsii.String("codePipelineNameOutput"), &awscdk.CfnOutputProps{
		Value: codePipelineV1.PipelineName(),
	})
	awscdk.NewCfnOutput(stack, jsii.String("CodeBuildProjectOuput"), &awscdk.CfnOutputProps{
		Value: codeBuildV1.ProjectName(),
	})

	awscdk.NewCfnOutput(stack, jsii.String("LambdaFunctionNameOutput"), &awscdk.CfnOutputProps{
		Value: lambdaFunctionV1.FunctionName(),
	})

	awscdk.NewCfnOutput(stack, jsii.String("LambdaFunctionArnOutput"), &awscdk.CfnOutputProps{
		Value: deadLetterQueue.QueueName(),
	})

	return stack
}

// Main() is the entry point of the CDK application
func main() {
	defer jsii.Close()

	app := awscdk.NewApp(nil)
	NewPipelineBuildV1(app, "CodePipelineCdkStack", &PipelineBuildV1Props{
		awscdk.StackProps{
			Env: env(),
		},
	})

	app.Synth(nil)
}

func env() *awscdk.Environment {
	return &awscdk.Environment{
		Account: jsii.String(checkEnv("ACCOUNT_ID")),
		Region:  jsii.String(checkEnv("AWS_REGION")),
	}
}
