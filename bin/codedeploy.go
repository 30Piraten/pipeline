package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodedeploy"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/jsii-runtime-go"
)

// CodeDeploy related resources
func createCodeDeployResources(resources *PipelineResources, lambdaAlias awslambda.Alias, lambdaFunction awslambda.Function) (awscodedeploy.LambdaApplication, awscodedeploy.LambdaDeploymentGroup) {
	// Create CodeDeploy application
	codeDeployApp := awscodedeploy.NewLambdaApplication(resources.stack, jsii.String("LambdaDeployV1"), &awscodedeploy.LambdaApplicationProps{
		ApplicationName: jsii.String(checkEnv("CODE_DEPLOY_APP_NAME")),
	})

	// Create deployment group with alarms
	lambdaErrorsAlarm := createLambdaErrorAlarm(resources.stack, lambdaFunction)
	deploymentGroup := createDeploymentGroup(resources.stack, codeDeployApp, lambdaAlias, lambdaErrorsAlarm)

	return codeDeployApp, deploymentGroup
}

func createDeploymentGroup(stack awscdk.Stack, app awscodedeploy.LambdaApplication,
	alias awslambda.Alias, errorAlarm awscloudwatch.IAlarm) awscodedeploy.LambdaDeploymentGroup {
	return awscodedeploy.NewLambdaDeploymentGroup(stack, jsii.String("BGCDeployment"),
		&awscodedeploy.LambdaDeploymentGroupProps{
			Application:      app,
			Alias:            alias,
			DeploymentConfig: awscodedeploy.LambdaDeploymentConfig_CANARY_10PERCENT_5MINUTES(),
			AutoRollback: &awscodedeploy.AutoRollbackConfig{
				FailedDeployment:  jsii.Bool(true),
				StoppedDeployment: jsii.Bool(true),
				DeploymentInAlarm: jsii.Bool(true),
			},
			Alarms: &[]awscloudwatch.IAlarm{errorAlarm},
		})
}
