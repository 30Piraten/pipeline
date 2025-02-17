package main

import (
	"log"
	"os"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodebuild"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipeline"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipelineactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3assets"
	"github.com/aws/jsii-runtime-go"
	"github.com/joho/godotenv"

	"github.com/aws/constructs-go/constructs/v10"
)

type PipelineBuildV1Props struct {
	awscdk.StackProps
}

func NewPipelineBuildV1(scope constructs.Construct, id string, props *PipelineBuildV1Props) awscdk.Stack {
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Define the Lambda function
	lambdaFunctionV1 := awslambda.NewFunction(stack, jsii.String("pipelineHandler"), &awslambda.FunctionProps{
		Runtime: awslambda.Runtime_PROVIDED_AL2(),
		Handler: jsii.String("bootstrap"),
		Code:    awslambda.Code_FromAsset(jsii.String("./lib/pipeline"), &awss3assets.AssetOptions{}),
	})

	// Funtion URL
	lambdaFunctionURL := lambdaFunctionV1.AddFunctionUrl(&awslambda.FunctionUrlOptions{
		AuthType: awslambda.FunctionUrlAuthType_AWS_IAM,
	})

	// CodeBuild Project
	codeBuildV1 := awscodebuild.NewProject(stack, jsii.String("CodeBuildV1"), &awscodebuild.ProjectProps{
		Source: awscodebuild.Source_GitHub(&awscodebuild.GitHubSourceProps{
			Owner:   jsii.String(os.Getenv("GITHUB_OWNER")),
			Repo:    jsii.String(os.Getenv("GITHUB_REPO")),
			Webhook: jsii.Bool(true),
		}),
		BuildSpec: awscodebuild.BuildSpec_FromSourceFilename(jsii.String("codebuild.yaml")),
		Environment: &awscodebuild.BuildEnvironment{
			BuildImage: awscodebuild.LinuxBuildImage_STANDARD_7_0(),
		},
	})

	// Define GitHub token secret
	tokenSecret := os.Getenv("GITHUB_TOKEN_SECRET")
	if tokenSecret == "" {
		log.Fatal("Error: GITHUB_TOKEN_SECRET variable is not defined.")
	}
	secretOptions := &awscdk.SecretsManagerSecretOptions{}

	// CodePipeline Construct
	codePipelineV1 := awscodepipeline.NewPipeline(stack, jsii.String("pipelineV1"), &awscodepipeline.PipelineProps{
		PipelineName: jsii.String("CodeBuildPipeline"),
		Stages: &[]*awscodepipeline.StageProps{
			{
				StageName: jsii.String("Source"),
				Actions: &[]awscodepipeline.IAction{
					awscodepipelineactions.NewGitHubSourceAction(&awscodepipelineactions.GitHubSourceActionProps{
						ActionName: jsii.String(os.Getenv("GITHUB_SOURCE")),
						Owner:      jsii.String(os.Getenv("GITHUB_OWNER")),
						Repo:       jsii.String(os.Getenv("GITHUB_REPO")),
						OauthToken: awscdk.SecretValue_SecretsManager(jsii.String(tokenSecret), secretOptions),
						Output:     awscodepipeline.NewArtifact(jsii.String("SourceArtifact")),
					}),
				},
			},
			{
				StageName: jsii.String("Build"),
				Actions: &[]awscodepipeline.IAction{
					awscodepipelineactions.NewCodeBuildAction(&awscodepipelineactions.CodeBuildActionProps{
						ActionName: jsii.String("Build"),
						Project:    codeBuildV1,
						Input:      awscodepipeline.NewArtifact(jsii.String("SourceArtifact")),
					}),
				},
			},
		},
	})

	// CloudWatch Construct
	metricNamespace := os.Getenv("METRIC_NAMESPACE")
	if metricNamespace == "" {
		log.Fatal("Error: METRIC_NAMESPACE variable for CloudWatch is not defined.")
	}
	metricName := os.Getenv("METRIC_NAME")
	if metricName == "" {
		log.Fatal("Error: METRIC_NAME variable for CloudWatch is not defined.")
	}

	awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
		Namespace:  jsii.String(metricNamespace),
		MetricName: jsii.String(metricName),
		DimensionsMap: &map[string]*string{
			"FunctionName": lambdaFunctionV1.FunctionName(),
		},
	})

	// CloudFormation Ouput
	awscdk.NewCfnOutput(stack, jsii.String("lambdaFunctionURL"), &awscdk.CfnOutputProps{
		Value: lambdaFunctionURL.Url(),
	})
	awscdk.NewCfnOutput(stack, jsii.String("codePipelineNameOutput"), &awscdk.CfnOutputProps{
		Value: codePipelineV1.PipelineName(),
	})
	awscdk.NewCfnOutput(stack, jsii.String("CodeBuildProjectOuput"), &awscdk.CfnOutputProps{
		Value: codeBuildV1.ProjectName(),
	})

	return stack
}

func main() {
	defer jsii.Close()

	// Load .env variables one time
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Warning: .env file not found or could not be loaded", err)
	}

	app := awscdk.NewApp(nil)

	NewPipelineBuildV1(app, "CdkStack", &PipelineBuildV1Props{
		awscdk.StackProps{
			Env: env(),
		},
	})

	app.Synth(nil)
}

func env() *awscdk.Environment {
	accountID := os.Getenv("ACCOUNT_ID")
	if accountID == "" {
		log.Fatal("Error: ACCOUNT_ID environment variable is required.")
	}

	accountRegion := os.Getenv("ACCOUNT_REGION")
	if accountRegion == "" {
		log.Fatal("Error: ACCOUNT_REGION environment variable is required.")
	}

	return &awscdk.Environment{
		Account: jsii.String(accountID),
		Region:  jsii.String(accountRegion),
	}
}
