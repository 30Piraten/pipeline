package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

func NewPipelineBuildV1(scope constructs.Construct, id string, props *PipelineBuildV1Props) awscdk.Stack {
	// Initialize stack
	stack := initializeStack(scope, id, props)

	// Create common resources
	resources := &PipelineResources{
		stack:          stack,
		githubSecret:   createGithubSecret(stack),
		artifactBucket: createArtifactBucket(stack),
		alarmTopic:     createMonitoringResources(stack),
	}

	// Create all service resources
	lambdaFunction, lambdaAlias := createLambdaResources(resources)
	codeDeployApp, deploymentGroup := createCodeDeployResources(resources, lambdaAlias, lambdaFunction)

	codeBuildProject := createCodeBuildResources(*resources)
	pipeline := createPipelineResources(resources, lambdaFunction, codeBuildProject)

	// Create CloudFormation outputs
	createStackOutputs(stack, *pipeline, codeBuildProject, lambdaFunction)

	// New outputs for CodeDeploy resources
	awscdk.NewCfnOutput(stack, jsii.String("CodeDeployApplicationOutput"), &awscdk.CfnOutputProps{
		Value:       codeDeployApp.ApplicationName(),
		Description: jsii.String("CodeDeploy Application Name"),
	})

	awscdk.NewCfnOutput(stack, jsii.String("CodeDeployDeploymentGroupOutput"), &awscdk.CfnOutputProps{
		Value:       deploymentGroup.DeploymentGroupName(),
		Description: jsii.String("CodeDeploy Deployment Group Name"),
	})

	return stack
}
