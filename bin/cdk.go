package main

import (
	"log"
	"os"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodebuild"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipeline"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipelineactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3assets"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/jsii-runtime-go"
	"github.com/joho/godotenv"

	"github.com/aws/constructs-go/constructs/v10"
)

type PipelineBuildV1Props struct {
	awscdk.StackProps
}

func checkEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("WARNING: %s environment variable is required!", key)
	}
	return value
}

func NewPipelineBuildV1(scope constructs.Construct, id string, props *PipelineBuildV1Props) awscdk.Stack {

	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Get GitHub Owner and Repo from environment variables
	githubOwner := checkEnv("GITHUB_OWNER")
	githubRepo := checkEnv("GITHUB_REPO")

	// Secret Manager definition
	githubSecret := awssecretsmanager.Secret_FromSecretNameV2(stack, jsii.String("GitHubTokenSecret"), jsii.String("token"))
	oauthTokenSecret := githubSecret.SecretValue()

	// Define IAM role for CodeBuild
	cloudBuildRoleV1 := awsiam.NewRole(stack, jsii.String("CodeBuildRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codebuild.amazonaws.com"), nil),
	})

	// Grant Secrets Manager access to CodeBuild
	cloudBuildRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
		Resources: jsii.Strings(*githubSecret.SecretArn()),
	}))

	// CodeBuild Project
	codeBuildV1 := awscodebuild.NewProject(stack, jsii.String("CodeBuildV1"), &awscodebuild.ProjectProps{
		Source: awscodebuild.Source_GitHub(&awscodebuild.GitHubSourceProps{
			Owner:   jsii.String(githubOwner),
			Repo:    jsii.String(githubRepo),
			Webhook: jsii.Bool(false),
		}),
		BuildSpec: awscodebuild.BuildSpec_FromSourceFilename(jsii.String("codebuild.yaml")),
		Role:      cloudBuildRoleV1,
		Environment: &awscodebuild.BuildEnvironment{
			BuildImage: awscodebuild.LinuxBuildImage_STANDARD_7_0(),
			EnvironmentVariables: &map[string]*awscodebuild.BuildEnvironmentVariable{
				"GITHUB_TOKEN": {
					Value: githubSecret.SecretArn(), // SecretARN here
					Type:  awscodebuild.BuildEnvironmentVariableType_SECRETS_MANAGER,
				},
			},
		},
	})

	// CodePipeline Construct
	codePipelineV1 := awscodepipeline.NewPipeline(stack, jsii.String("pipelineV1"), &awscodepipeline.PipelineProps{
		PipelineName: jsii.String("CodeBuildPipeline"),
		Stages: &[]*awscodepipeline.StageProps{
			{
				StageName: jsii.String("Source"),
				Actions: &[]awscodepipeline.IAction{
					awscodepipelineactions.NewGitHubSourceAction(&awscodepipelineactions.GitHubSourceActionProps{
						ActionName: jsii.String("pipelineSource"),
						Owner:      jsii.String(githubOwner),
						Repo:       jsii.String(githubRepo),
						Branch:     jsii.String("main"),
						OauthToken: oauthTokenSecret, // Passed here
						Output:     awscodepipeline.NewArtifact(jsii.String("SourceArtifact")),
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
						Input:      awscodepipeline.NewArtifact(jsii.String("SourceArtifact")),
					}),
				},
			},
		},
	})

	// Define the Lambda function
	lambdaFunctionV1 := awslambda.NewFunction(stack, jsii.String("pipelineHandler"), &awslambda.FunctionProps{
		Runtime: awslambda.Runtime_PROVIDED_AL2(),
		Handler: jsii.String("bootstrap"),
		Code:    awslambda.Code_FromAsset(jsii.String("./lambda/"), &awss3assets.AssetOptions{}),
		Environment: &map[string]*string{
			"GITHUB_TOKEN": githubSecret.SecretArn(), // SecretARN here
		},
	})

	// lambdaFunctionV1.Role().AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
	// 	Effect:    awsiam.Effect_ALLOW,
	// 	Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
	// 	Resources: jsii.Strings(*githubSecret.SecretArn()), // SecretARN here
	// }))

	// lambdaFuntionURL
	lambdaFunctionURL := lambdaFunctionV1.AddFunctionUrl(&awslambda.FunctionUrlOptions{
		// AuthType: awslambda.FunctionUrlAuthType_AWS_IAM,
		AuthType: awslambda.FunctionUrlAuthType_NONE,
	})

	// CloudWatch Construct
	metricNamespace := checkEnv("METRIC_NAMESPACE")
	metricName := checkEnv("METRIC_NAME")

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
	if err := godotenv.Load(); err != nil {
		log.Fatal("Warning: .env file not found or could not be loaded", err)
	}

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
		Region:  jsii.String(checkEnv("ACCOUNT_REGION")),
	}
}
