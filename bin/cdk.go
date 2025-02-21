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
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3assets"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/jsii-runtime-go"

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

	// Secret Manager definition
	githubSecret := awssecretsmanager.Secret_FromSecretNameV2(stack, jsii.String("GitHubTokenSecret"), jsii.String("token"))
	oauthTokenSecret := githubSecret.SecretValue()

	// LAMBDA LOGIC DEFINITION
	// Define File path dir --> Specific for Lambda function file path
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
		// must come from --> awslambda.Code_FromCfnParatemers() since Lambda is only used as deployment strategy
		Code: awslambda.Code_FromAsset(jsii.String(lambdaDir), &awss3assets.AssetOptions{}),
		Environment: &map[string]*string{
			"GITHUB_TOKEN": githubSecret.SecretArn(), // SecretARN here,
		},
	})

	// CODEDEPLOY LOGIC DEFINITION
	// CodeDeploy + CloudWatch Alarm for rollback
	codeDeployV1 := awscodedeploy.NewLambdaApplication(stack, jsii.String("LambdaDeployV1"), &awscodedeploy.LambdaApplicationProps{
		ApplicationName: jsii.String("codeDeployLambdaV1"),
	})

	// Define a Lambda Alias for Deployment
	lambdaAlias := awslambda.NewAlias(stack, jsii.String("production"), &awslambda.AliasProps{
		AliasName: jsii.String("Live"),
		Version:   lambdaFunctionV1.CurrentVersion(),
	})

	// Define a Deployment application
	deploymentGroupV1 := awscodedeploy.NewLambdaDeploymentGroup(stack, jsii.String("BlueGreenDeployment"), &awscodedeploy.LambdaDeploymentGroupProps{
		Application:      codeDeployV1,
		Alias:            lambdaAlias,
		DeploymentConfig: awscodedeploy.LambdaDeploymentConfig_LINEAR_10PERCENT_EVERY_1MINUTE(),
		AutoRollback: &awscodedeploy.AutoRollbackConfig{
			DeploymentInAlarm: jsii.Bool(true),
			FailedDeployment:  jsii.Bool(true),
		},
	})

	// Restrict Lambda's access to GitHub secret
	lambdaFunctionV1.Role().AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
		Resources: jsii.Strings(*githubSecret.SecretArn()), // SecretARN here
	}))

	lambdaFunctionV1.Role().AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("codedeploy:CreateDeployment", "codedeploy:GetDeploymentConfig", "codedeploy:ApplicationRevision"),
		Resources: jsii.Strings(*deploymentGroupV1.DeploymentGroupArn()),
	}))

	// Grant AWS CodePipeline to invoke Lambda
	lambdaFunctionV1.GrantInvoke(awsiam.NewServicePrincipal(jsii.String("codepipeline.amazonaws.com"), nil))

	// Since the Lambda function takes a secretARN, we need secure it
	// I don't see the reason for this yet! Not necessary, but will let horse around a bit.
	lambdaFunctionURL := lambdaFunctionV1.AddFunctionUrl(&awslambda.FunctionUrlOptions{
		AuthType: awslambda.FunctionUrlAuthType_AWS_IAM,
	})

	// CODEBUILD LOGIC DEFINITION
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
			Owner:   jsii.String("30Piraten"),
			Repo:    jsii.String("pipeline"),
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

	// CODEPIPELINE LOGIC DEFINITION
	// CodePipeline Construct
	codePipelineV1 := awscodepipeline.NewPipeline(stack, jsii.String("pipelineV1"), &awscodepipeline.PipelineProps{
		PipelineName: jsii.String("CodeBuildPipeline"),
		Stages: &[]*awscodepipeline.StageProps{
			{
				StageName: jsii.String("Source"),
				Actions: &[]awscodepipeline.IAction{
					awscodepipelineactions.NewGitHubSourceAction(&awscodepipelineactions.GitHubSourceActionProps{
						ActionName: jsii.String("pipelineSource"),
						Owner:      jsii.String("30Piraten"),
						Repo:       jsii.String("pipeline"),
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
						Outputs:    &[]awscodepipeline.Artifact{awscodepipeline.NewArtifact(jsii.String("BuildArtifact"))},
					}),
				},
			},
			{
				StageName: jsii.String("Deploy"),
				Actions: &[]awscodepipeline.IAction{
					// Create & update the Lambda function via CloudFormation
					awscodepipelineactions.NewCloudFormationCreateReplaceChangeSetAction(&awscodepipelineactions.CloudFormationCreateReplaceChangeSetActionProps{
						ActionName:       jsii.String("PrepareChanges"),
						StackName:        jsii.String("LambdaDeploymentStack"),
						ChangeSetName:    jsii.String("LambdaDeploymentChangeSet"),
						TemplatePath:     awscodepipeline.NewArtifact(jsii.String("BuildOutput")).AtPath(jsii.String("template.yaml")),
						AdminPermissions: jsii.Bool(true),
						ExtraInputs: &[]awscodepipeline.Artifact{
							awscodepipeline.NewArtifact(jsii.String("BuildOutput")),
						},
					}),
					awscodepipelineactions.NewCloudFormationExecuteChangeSetAction(&awscodepipelineactions.CloudFormationExecuteChangeSetActionProps{
						ActionName:    jsii.String("UpdateChanges"),
						StackName:     jsii.String("LambdaDeploymentStack"),
						ChangeSetName: jsii.String("LambdaDeploymentChangeSet"),
					}),
				},
			},
		},
	})

	// CLOUDWATCH LOGIC DEFINITION
	// CloudWatch Construct
	rollbackAlarm := awscloudwatch.NewAlarm(stack, jsii.String("LambdaDeploymentAlarm"), &awscloudwatch.AlarmProps{
		Metric:             lambdaFunctionV1.MetricErrors(&awscloudwatch.MetricOptions{}),
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_THRESHOLD,
		Threshold:          jsii.Number(5), // Need to review!
		EvaluationPeriods:  jsii.Number(1),
		AlarmName:          jsii.String("LambdaDeploymentFailure"),
	})

	// Attach the Alarm to deployment group
	deploymentGroupV1.AddAlarm(rollbackAlarm)

	awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
		Namespace:  jsii.String("Invocations"),
		MetricName: jsii.String("AWS/Lambda"),
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
	// if err := godotenv.Load(); err != nil {
	// 	log.Fatal("Warning: .env file not found or could not be loaded", err)
	// }

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
		// Account: jsii.String(checkEnv("ACCOUNT_ID")),
		// Region:  jsii.String(checkEnv("ACCOUNT_REGION")),
		Account: jsii.String(os.Getenv("ACCOUNT_ID")),
		Region:  jsii.String("us-east-1"),
	}
}
