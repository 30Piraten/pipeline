package main

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3assets"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssqs"
	"github.com/aws/jsii-runtime-go"
)

// Lambda related resources
func createLambdaResources(resources *PipelineResources) (awslambda.Function, awslambda.Alias) {
	// Create DLQ
	deadLetterQueue := createDeadLetterQueue(resources.stack)

	// Create Lambda function
	lambdaFunction := createLambdaFunction(resources.stack, resources.githubSecret, deadLetterQueue)

	// Create Lambda alias
	lambdaAlias := awslambda.NewAlias(resources.stack, jsii.String("production"), &awslambda.AliasProps{
		AliasName:   jsii.String("Live"),
		Description: jsii.String("Lambda Alias"),
		Version:     lambdaFunction.CurrentVersion(),
	})

	// Configure Lambda IAM roles
	configureLambdaIAM(resources.stack, lambdaFunction, resources.githubSecret)

	return lambdaFunction, lambdaAlias
}

func createDeadLetterQueue(stack awscdk.Stack) awssqs.IQueue {
	return awssqs.NewQueue(stack, jsii.String("LambdaDLQ"), &awssqs.QueueProps{
		QueueName:       jsii.String("lambda-deploy-dlq"),
		RetentionPeriod: awscdk.Duration_Days(jsii.Number(7)),
	})
}

func createLambdaFunction(stack awscdk.Stack, githubSecret awssecretsmanager.ISecret, dlq awssqs.IQueue) awslambda.Function {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("Could not get file name")
	}
	lambdaDir := filepath.Join(filepath.Dir(filename), "lambda")

	return awslambda.NewFunction(stack, jsii.String("pipelineHandler"), &awslambda.FunctionProps{
		Runtime:         awslambda.Runtime_PROVIDED_AL2(),
		Handler:         jsii.String("bootstrap"),
		RetryAttempts:   jsii.Number(2),
		MemorySize:      jsii.Number(1024),
		Timeout:         awscdk.Duration_Minutes(jsii.Number(6)),
		Architecture:    awslambda.Architecture_X86_64(),
		DeadLetterQueue: dlq,
		CurrentVersionOptions: &awslambda.VersionOptions{
			RemovalPolicy: awscdk.RemovalPolicy_RETAIN,
			Description:   jsii.String("Automated Version"),
		},
		Code: awslambda.Code_FromAsset(jsii.String(lambdaDir), &awss3assets.AssetOptions{}),
		Environment: &map[string]*string{
			"GITHUB_TOKEN":             githubSecret.SecretArn(),
			"APPLICATION_NAME":         jsii.String("LambdaDeployApp"),
			"DEPLOYMENT_GROUP_NAME":    jsii.String("LambdaDeploymentGroup"),
			"MAX_DEPLOYMENT_WAIT_TIME": jsii.String("600"),
		},
		Tracing: awslambda.Tracing_ACTIVE,
	})
}

func createLambdaErrorAlarm(stack awscdk.Stack, lambdaFunction awslambda.Function) awscloudwatch.Alarm {
	return awscloudwatch.NewAlarm(stack, jsii.String("LambdaErrorsAlarm"), &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Alarm for Lambda errors"),
		AlarmName:        jsii.String("LambdaErrorsAlarm"),
		Metric: awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
			Namespace:  jsii.String("AWS/Lambda"),
			MetricName: jsii.String("Errors"),
			Statistic:  jsii.String("Sum"),
			Period:     awscdk.Duration_Minutes(jsii.Number(1)),
			DimensionsMap: &map[string]*string{
				"FunctionName": lambdaFunction.FunctionName(),
			},
		}),
		EvaluationPeriods:  jsii.Number(1),
		Threshold:          jsii.Number(1),
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD,
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,
	})
}

func configureLambdaIAM(stack awscdk.Stack, lambdaFunction awslambda.Function, githubSecret awssecretsmanager.ISecret) {
	lambdaRole := awsiam.NewRole(stack, jsii.String("LambdaRoleV1"), &awsiam.RoleProps{
		AssumedBy: awsiam.NewServicePrincipal(jsii.String("lambda.amazonaws.com"), nil),
	})

	// Grant permissions for GitHub secret
	lambdaRole.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect:    awsiam.Effect_ALLOW,
		Actions:   jsii.Strings("secretsmanager:GetSecretValue"),
		Resources: jsii.Strings(*githubSecret.SecretArn()),
	}))

	// Grant CloudWatch permissions
	lambdaRole.AddToPrincipalPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: jsii.Strings(
			"logs:CreateLogGroup",
			"logs:CreateLogStream",
			"logs:PutLogEvents",
		),
		Resources: jsii.Strings(
			fmt.Sprintf("arn:aws:logs:%s:%s:log-group:/aws/lambda/%s:*",
				*stack.Region(), *stack.Account(), *lambdaFunction.FunctionName()),
		),
	}))
}
