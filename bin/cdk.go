package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodebuild"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodedeploy"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipeline"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipelineactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3assets"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
	"github.com/joho/godotenv"
)

type PipelineBuildV1Props struct {
	awscdk.StackProps
}

func checkEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Printf("WARNING: %s environment variable is required!", key)
	}
	return value
}

func NewPipelineBuildV1(scope constructs.Construct, id string, props *PipelineBuildV1Props) awscdk.Stack {

	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Secret Manager definition
	githubSecret := awssecretsmanager.Secret_FromSecretNameV2(stack, jsii.String("GitHubTokenSecret"), jsii.String("token"))
	oauthTokenSecret := githubSecret.SecretValue()

	// LAMBDA LOGIC DEFINITION
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("Could not get file name")
	}
	lambdaDir := filepath.Join(filepath.Dir(filename), "lambda")

	// Define the Lambda function
	lambdaFunctionV1 := awslambda.NewFunction(stack, jsii.String("pipelineHandler"), &awslambda.FunctionProps{
		Runtime:                awslambda.Runtime_PROVIDED_AL2(),
		Handler:                jsii.String("bootstrap"),
		RetryAttempts:          jsii.Number(2),
		MemorySize:             jsii.Number(1024),
		Timeout:                awscdk.Duration_Seconds(jsii.Number(30)),
		Architecture:           awslambda.Architecture_X86_64(),
		DeadLetterQueueEnabled: jsii.Bool(true),
		CurrentVersionOptions: &awslambda.VersionOptions{
			RemovalPolicy: awscdk.RemovalPolicy_RETAIN,
			Description:   jsii.String("Automated Version"),
		},
		Code: awslambda.Code_FromAsset(jsii.String(lambdaDir), &awss3assets.AssetOptions{}),
		Environment: &map[string]*string{
			"GITHUB_TOKEN":          githubSecret.SecretArn(),
			"APPLICATION_NAME":      jsii.String("LambdaDeployApp"),
			"DEPLOYMENT_GROUP_NAME": jsii.String("LambdaDeploymentGroup"),
		},
	})

	// Define a Lambda Alias for Deployment
	lambdaAlias := awslambda.NewAlias(stack, jsii.String("production"), &awslambda.AliasProps{
		AliasName:   jsii.String("Live"),
		Description: jsii.String("Lambda Alias"),
		Version:     lambdaFunctionV1.CurrentVersion(),
	})

	// CODEDEPLOY LOGIC DEFINITION
	codeDeployV1 := awscodedeploy.NewLambdaApplication(stack, jsii.String("LambdaDeployV1"), &awscodedeploy.LambdaApplicationProps{
		ApplicationName: jsii.String("codeDeployLambdaV1"),
	})

	// Define a Deployment application
	deploymentGroupV1 := awscodedeploy.NewLambdaDeploymentGroup(stack, jsii.String("BGCDeployment"), &awscodedeploy.LambdaDeploymentGroupProps{
		Application:      codeDeployV1,
		Alias:            lambdaAlias,
		DeploymentConfig: awscodedeploy.LambdaDeploymentConfig_CANARY_10PERCENT_5MINUTES(),
		AutoRollback: &awscodedeploy.AutoRollbackConfig{
			FailedDeployment:  jsii.Bool(true),
			StoppedDeployment: jsii.Bool(true),
		},
	})

	// Restrict Lambda's access to GitHub secret
	lambdaFunctionV1.Role().AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
		Resources: jsii.Strings(*githubSecret.SecretArn()),
	}))

	lambdaFunctionV1.Role().AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("codedeploy:CreateDeployment", "codedeploy:GetDeploymentConfig", "codedeploy:ApplicationRevision", "codedeploy:GetDeployment", "codedeploy:UpdateDeployment"),
		Resources: jsii.Strings(*deploymentGroupV1.DeploymentGroupArn()),
	}))

	// Grant AWS CodePipeline to invoke Lambda
	lambdaAlias.GrantInvoke(awsiam.NewServicePrincipal(jsii.String("codepipeline.amazonaws.com"), nil))

	// CODEBUILD LOGIC DEFINITION
	// Define IAM role for CodeBuild
	codeBuildRoleV1 := awsiam.NewRole(stack, jsii.String("CodeBuildRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codebuild.amazonaws.com"), nil),
	})

	codeBuildV1 := awscodebuild.NewProject(stack, jsii.String("CodeBuildV1"), &awscodebuild.ProjectProps{
		Source: awscodebuild.Source_GitHub(&awscodebuild.GitHubSourceProps{
			Owner: jsii.String("30Piraten"),
			Repo:  jsii.String("pipeline"),
		}),
		BuildSpec: awscodebuild.BuildSpec_FromSourceFilename(jsii.String("cdk/buildspec.yml")),
		Role:      codeBuildRoleV1,
		Environment: &awscodebuild.BuildEnvironment{
			ComputeType: awscodebuild.ComputeType_SMALL,

			BuildImage: awscodebuild.LinuxBuildImage_AMAZON_LINUX_2_3(),
			EnvironmentVariables: &map[string]*awscodebuild.BuildEnvironmentVariable{
				"GITHUB_TOKEN": {
					Value: githubSecret.SecretArn(),
					Type:  awscodebuild.BuildEnvironmentVariableType_SECRETS_MANAGER,
				},
			},
		},
	})

	// Grant Secrets Manager access to CodeBuild
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
		Resources: jsii.Strings("*"),
	}))

	// CODEPIPELINE LOGIC DEFINITION
	codePipelineRoleV1 := awsiam.NewRole(stack, jsii.String("CodePipelineRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codepipeline.amazonaws.com"), nil),
	})

	// S3 bucket for Artifact
	artifactS3Bucket := awss3.NewBucket(stack, jsii.String("ArtifactBucket"), &awss3.BucketProps{
		AutoDeleteObjects: jsii.Bool(true),
		RemovalPolicy:     awscdk.RemovalPolicy_DESTROY,
	})

	sourceArtifact := awscodepipeline.NewArtifact(jsii.String("SourceArtifact"))
	buildArtifact := awscodepipeline.NewArtifact(jsii.String("BuildArtifact"))

	codePipelineV1 := awscodepipeline.NewPipeline(stack, jsii.String("pipelineV1"), &awscodepipeline.PipelineProps{
		PipelineName:   jsii.String("CodeBuildPipeline"),
		ArtifactBucket: artifactS3Bucket,
		Stages: &[]*awscodepipeline.StageProps{
			{
				StageName: jsii.String("Source"),
				Actions: &[]awscodepipeline.IAction{
					awscodepipelineactions.NewGitHubSourceAction(&awscodepipelineactions.GitHubSourceActionProps{
						ActionName: jsii.String("pipelineSource"),
						Owner:      jsii.String("30Piraten"),
						Repo:       jsii.String("pipeline"),
						Branch:     jsii.String("main"),
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
	})

	// Grant necessary permissions
	artifactS3Bucket.GrantReadWrite(codePipelineRoleV1, nil)
	codePipelineRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("iam:PassRole"),
		Resources: jsii.Strings(*deploymentGroupV1.Role().RoleArn()),
	}))

	codePipelineRoleV1.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"codeploy:CreateDeployment",
			"codedeploy:GetDeploymentConfig",
			"codedeploy:GetDeployment",
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

	awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
		Namespace:  jsii.String("Invocations"),
		MetricName: jsii.String("AWS/Lambda"),
		DimensionsMap: &map[string]*string{
			"FunctionName": lambdaFunctionV1.FunctionName(),
		},
	})

	// CloudFormation Ouput
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

	// // Load .env variables one time
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
		Region:  jsii.String("us-east-1"),
	}
}
