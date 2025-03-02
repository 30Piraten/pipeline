package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatchactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodebuild"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipeline"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipelineactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/jsii-runtime-go"
)

// Pipeline related resources
func createPipelineResources(resources *PipelineResources, lambdaFunction awslambda.Function, codeBuildProject awscodebuild.Project) *awscodepipeline.Pipeline {
	// Create pipeline role
	pipelineRole := createPipelineRole(resources.stack)

	// Create artifacts
	sourceArtifact := awscodepipeline.NewArtifact(jsii.String("SourceArtifact"), nil)
	buildArtifact := awscodepipeline.NewArtifact(jsii.String("BuildArtifact"), nil)

	// Create pipeline
	pipeline := createPipeline(resources, pipelineRole, sourceArtifact, buildArtifact, lambdaFunction, codeBuildProject)

	// Create pipeline alarms
	pipelineAlarm := createPipelineAlarm(resources.stack, pipeline)
	pipelineAlarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(resources.alarmTopic))

	return &pipeline
}

func createPipelineRole(stack awscdk.Stack) awsiam.Role {
	return awsiam.NewRole(stack, jsii.String("CodePipelineRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codepipeline.amazonaws.com"), nil),
	})
}

func createPipeline(resources *PipelineResources, pipelineRole awsiam.IRole,
	sourceArtifact awscodepipeline.Artifact, buildArtifact awscodepipeline.Artifact,
	lambdaFunction awslambda.Function, codeBuildProject awscodebuild.Project) awscodepipeline.Pipeline {

	return awscodepipeline.NewPipeline(resources.stack, jsii.String("pipelineV1"),
		&awscodepipeline.PipelineProps{
			PipelineName:   jsii.String("CodeBuildPipelineV1"),
			ArtifactBucket: resources.artifactBucket,
			Role:           pipelineRole,
			Stages: &[]*awscodepipeline.StageProps{
				createSourceStage(sourceArtifact, resources.githubSecret),
				createBuildStage(buildArtifact, sourceArtifact, codeBuildProject),
				createDeployStage(buildArtifact, lambdaFunction),
			},
			CrossAccountKeys: jsii.Bool(false),
		})
}

func createSourceStage(sourceArtifact awscodepipeline.Artifact,
	githubSecret awssecretsmanager.ISecret) *awscodepipeline.StageProps {
	return &awscodepipeline.StageProps{
		StageName: jsii.String("Source"),
		Actions: &[]awscodepipeline.IAction{
			awscodepipelineactions.NewGitHubSourceAction(&awscodepipelineactions.GitHubSourceActionProps{
				ActionName: jsii.String("pipelineSource"),
				Owner:      jsii.String(checkEnv("GITHUB_OWNER")),
				Repo:       jsii.String(checkEnv("GITHUB_REPO")),
				Branch:     jsii.String(checkEnv("GITHUB_BRANCH")),
				OauthToken: githubSecret.SecretValue(),
				Output:     sourceArtifact,
				Trigger:    awscodepipelineactions.GitHubTrigger_WEBHOOK,
			}),
		},
	}
}

func createBuildStage(buildArtifact awscodepipeline.Artifact,
	sourceArtifact awscodepipeline.Artifact,
	codeBuildProject awscodebuild.Project) *awscodepipeline.StageProps {
	return &awscodepipeline.StageProps{
		StageName: jsii.String("Build"),
		Actions: &[]awscodepipeline.IAction{
			awscodepipelineactions.NewCodeBuildAction(&awscodepipelineactions.CodeBuildActionProps{
				ActionName: jsii.String("pipelineBuild"),
				Project:    codeBuildProject,
				Input:      sourceArtifact,
				Outputs:    &[]awscodepipeline.Artifact{buildArtifact},
			}),
		},
	}
}

func createDeployStage(buildArtifact awscodepipeline.Artifact,
	lambdaFunction awslambda.Function) *awscodepipeline.StageProps {
	return &awscodepipeline.StageProps{
		StageName: jsii.String("Deploy"),
		Actions: &[]awscodepipeline.IAction{
			awscodepipelineactions.NewLambdaInvokeAction(&awscodepipelineactions.LambdaInvokeActionProps{
				ActionName: jsii.String("DeployLambda"),
				Inputs:     &[]awscodepipeline.Artifact{buildArtifact},
				Lambda:     lambdaFunction,
			}),
		},
	}
}

func createPipelineAlarm(stack awscdk.Stack, pipeline awscodepipeline.Pipeline) awscloudwatch.Alarm {
	return awscloudwatch.NewAlarm(stack, jsii.String("PipelineFailureAlarm"), &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Alert when CodePipeline project fails"),
		AlarmName:        jsii.String("PipelineFailureAlarm"),
		Metric: awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
			Namespace:  jsii.String("AWS/CodePipeline"),
			MetricName: jsii.String("FailedPipelines"),
			Statistic:  jsii.String("Sum"),
			Period:     awscdk.Duration_Minutes(jsii.Number(5)),
			DimensionsMap: &map[string]*string{
				"PipelineName": pipeline.PipelineName(),
			},
			Unit: awscloudwatch.Unit_COUNT,
		}),
		EvaluationPeriods:  jsii.Number(1),
		Threshold:          jsii.Number(1),
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD,
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,
	})
}
