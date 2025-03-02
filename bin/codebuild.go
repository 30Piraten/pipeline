package main

import (
	"os"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatchactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodebuild"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/jsii-runtime-go"
)

// CodeBuild related resources
func createCodeBuildResources(resources PipelineResources) awscodebuild.Project {
	// Create CodeBuild role
	codeBuildRole := createCodeBuildRole(resources.stack, resources.githubSecret)

	// Create CodeBuild project
	codeBuildProject := createCodeBuildProject(resources.stack, resources.githubSecret, codeBuildRole)

	// Create CodeBuild alarms
	codeBuildAlarm := createCodeBuildAlarm(resources.stack, codeBuildProject)
	codeBuildAlarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(resources.alarmTopic))

	return codeBuildProject
}

func createCodeBuildRole(stack awscdk.Stack, githubSecret awssecretsmanager.ISecret) awsiam.Role {
	role := awsiam.NewRole(stack, jsii.String("CodeBuildRole"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("codebuild.amazonaws.com"), nil),
	})

	role.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
		Resources: jsii.Strings(*githubSecret.SecretArn()),
	}))

	return role
}

func createCodeBuildProject(stack awscdk.Stack, githubSecret awssecretsmanager.ISecret,
	role awsiam.IRole) awscodebuild.Project {
	return awscodebuild.NewProject(stack, jsii.String("CodeBuildV1"), &awscodebuild.ProjectProps{
		Source: awscodebuild.Source_GitHub(&awscodebuild.GitHubSourceProps{
			Owner: jsii.String(os.Getenv("GITHUB_OWNER")),
			Repo:  jsii.String(os.Getenv("GITHUB_REPO")),
		}),
		BuildSpec:   awscodebuild.BuildSpec_FromSourceFilename(jsii.String("cdk/buildspec.yml")),
		Role:        role,
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
}

func createCodeBuildAlarm(stack awscdk.Stack, project awscodebuild.Project) awscloudwatch.Alarm {
	return awscloudwatch.NewAlarm(stack, jsii.String("CodeBuildFailureAlarm"), &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Alert when CodeBuild project fails"),
		AlarmName:        jsii.String("CodeBuildFailureAlarm"),
		Metric: awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
			Namespace:  jsii.String("AWS/CodeBuild"),
			MetricName: jsii.String("FailedBuilds"),
			Statistic:  jsii.String("Sum"),
			Period:     awscdk.Duration_Minutes(jsii.Number(5)),
			DimensionsMap: &map[string]*string{
				"ProjectName": project.ProjectName(),
			},
			Unit: awscloudwatch.Unit_COUNT,
		}),
		EvaluationPeriods:  jsii.Number(1),
		Threshold:          jsii.Number(1),
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD,
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,
	})
}
