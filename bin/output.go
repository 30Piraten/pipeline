package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodebuild"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscodepipeline"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/jsii-runtime-go"
)

func createStackOutputs(stack awscdk.Stack, pipeline awscodepipeline.Pipeline,
	codeBuildProject awscodebuild.Project, lambdaFunction awslambda.Function) {
	awscdk.NewCfnOutput(stack, jsii.String("codePipelineNameOutput"), &awscdk.CfnOutputProps{
		Value: pipeline.PipelineName(),
	})

	awscdk.NewCfnOutput(stack, jsii.String("CodeBuildProjectOutput"), &awscdk.CfnOutputProps{
		Value: codeBuildProject.ProjectName(),
	})

	awscdk.NewCfnOutput(stack, jsii.String("LambdaFunctionNameOutput"), &awscdk.CfnOutputProps{
		Value: lambdaFunction.FunctionName(),
	})
}
