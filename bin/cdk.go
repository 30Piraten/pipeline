package main

import (
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
)

type PipelineBuildV1Props struct {
	awscdk.StackProps
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
	// This is not necessary, but it need it for something else!
	synth := awscdk.NewDefaultStackSynthesizer(&awscdk.DefaultStackSynthesizerProps{
		Qualifier: jsii.String("pipeline-artifact-bucket-v1"),
	})

	sprops.Synthesizer = synth

	// Retrieve the GitHub token from Secrets Manager
	githubSecret := awssecretsmanager.Secret_FromSecretNameV2(stack, jsii.String("GitHubTokenSecret"), jsii.String("token"))
	oauthTokenSecret := githubSecret.SecretValue()

	// Get the Lambda function directory path
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("Could not get file name")
	}
	lambdaDir := filepath.Join(filepath.Dir(filename), "lambda")

	// Create the Lambda function with configuration
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

	// Create the Lambda alias for production deployment
	lambdaAlias := awslambda.NewAlias(stack, jsii.String("production"), &awslambda.AliasProps{
		AliasName:   jsii.String("Live"),
		Description: jsii.String("Lambda Alias"),
		Version:     lambdaFunctionV1.CurrentVersion(),
	})

	// Set up the CodeDeploy application for Lambda
	codeDeployV1 := awscodedeploy.NewLambdaApplication(stack, jsii.String("LambdaDeployV1"), &awscodedeploy.LambdaApplicationProps{
		ApplicationName: jsii.String("codeDeployLambdaV1"),
	})

	// Here, we configure the deployment group with canary deployment
	deploymentGroupV1 := awscodedeploy.NewLambdaDeploymentGroup(stack, jsii.String("BGCDeployment"), &awscodedeploy.LambdaDeploymentGroupProps{
		Application:      codeDeployV1,
		Alias:            lambdaAlias,
		DeploymentConfig: awscodedeploy.LambdaDeploymentConfig_CANARY_10PERCENT_5MINUTES(),
		AutoRollback: &awscodedeploy.AutoRollbackConfig{
			FailedDeployment:  jsii.Bool(true),
			StoppedDeployment: jsii.Bool(true),
		},
	})

	// Granting Lambda function permissions to access GitHub secret
	lambdaFunctionV1.Role().AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
		Resources: jsii.Strings(*githubSecret.SecretArn()),
	}))

	// Granting Lambda function permissions for CodeDeploy operations
	lambdaFunctionV1.Role().AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"codedeploy:CreateDeployment",
			"codedeploy:GetDeploymentConfig",
			"codedeploy:ApplicationRevision",
			"codedeploy:GetDeployment",
			"codedeploy:UpdateDeployment",
		),
		Resources: jsii.Strings(*deploymentGroupV1.DeploymentGroupArn()),
	}))

	lambdaFunctionV1.Role().AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"codepipeline:PutJobSuccessResult",
			"codepipeline:PutJobFailureResult",
		),
		Resources: jsii.Strings("*"),
	}))

	// We allow CodePipeline to invoke Lambda
	lambdaAlias.GrantInvoke(awsiam.NewServicePrincipal(jsii.String("codepipeline.amazonaws.com"), nil))

	// And create the IAM role for CodeBuild
	codeBuildRoleV1 := awsiam.NewRole(stack, jsii.String("CodeBuildRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codebuild.amazonaws.com"), nil),
	})

	// We set up the CodeBuild project
	codeBuildV1 := awscodebuild.NewProject(stack, jsii.String("CodeBuildV1"), &awscodebuild.ProjectProps{
		Source: awscodebuild.Source_GitHub(&awscodebuild.GitHubSourceProps{
			Owner: jsii.String("30Piraten"),
			Repo:  jsii.String("pipeline"),
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
		Resources: jsii.Strings("*"),
	}))

	// Create IAM role for CodePipeline
	codePipelineRoleV1 := awsiam.NewRole(stack, jsii.String("CodePipelineRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codepipeline.amazonaws.com"), nil),
	})

	// Create S3 bucket for artifacts
	artifactBucketV1 := awss3.NewBucket(stack, jsii.String("ArtifactBucket"), &awss3.BucketProps{
		AutoDeleteObjects: jsii.Bool(true),
		RemovalPolicy:     awscdk.RemovalPolicy_DESTROY,
		BucketName:        jsii.String("pipeline-artifact-bucket-v1"),
	})

	// Here, we define artifacts for the pipeline stages
	sourceArtifact := awscodepipeline.NewArtifact(jsii.String("SourceArtifact"), nil)
	buildArtifact := awscodepipeline.NewArtifact(jsii.String("BuildArtifact"), nil)

	// Create the CodePipeline with Source, Build, and Deploy stages
	codePipelineV1 := awscodepipeline.NewPipeline(stack, jsii.String("pipelineV1"), &awscodepipeline.PipelineProps{
		PipelineName:   jsii.String("CodeBuildPipelineV1"),
		ArtifactBucket: artifactBucketV1,
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

	// Granting permissions to CodePipeline role
	artifactBucketV1.GrantReadWrite(codePipelineRoleV1, nil)
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

	// Here, we create CloudWatch metric for Lambda invocations
	awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
		Namespace:  jsii.String("Invocations"),
		MetricName: jsii.String("AWS/Lambda"),
		DimensionsMap: &map[string]*string{
			"FunctionName": lambdaFunctionV1.FunctionName(),
		},
	})

	// We create the CloudFormation outputs
	awscdk.NewCfnOutput(stack, jsii.String("codePipelineNameOutput"), &awscdk.CfnOutputProps{
		Value: codePipelineV1.PipelineName(),
	})
	awscdk.NewCfnOutput(stack, jsii.String("CodeBuildProjectOuput"), &awscdk.CfnOutputProps{
		Value: codeBuildV1.ProjectName(),
	})

	return stack
}

// The main is the entry point of the CDK application
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
		Account: jsii.String(os.Getenv("ACCOUNT_ID")),
		// Region:  jsii.String(os.Getenv("AWS_REGION")),

		Region: jsii.String("us-east-1"),
	}
}
