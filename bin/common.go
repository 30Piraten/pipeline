package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

type PipelineBuildV1Props struct {
	awscdk.StackProps
}

// common.go
func initializeStack(scope constructs.Construct, id string, props *PipelineBuildV1Props) awscdk.Stack {
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}

	stack := awscdk.NewStack(scope, &id, &sprops)

	// Configure stack synthesizer
	synth := awscdk.NewDefaultStackSynthesizer(&awscdk.DefaultStackSynthesizerProps{
		Qualifier: jsii.String("pipeline-artifact-bucket-v1"),
	})
	sprops.Synthesizer = synth

	return stack
}

func createGithubSecret(stack awscdk.Stack) awssecretsmanager.ISecret {
	return awssecretsmanager.Secret_FromSecretNameV2(stack,
		jsii.String("GitHubTokenSecret"),
		jsii.String("token"))
}

func createArtifactBucket(stack awscdk.Stack) awss3.IBucket {
	return awss3.NewBucket(stack, jsii.String("S3_ARTIFACT_BUCKET_NAME"), &awss3.BucketProps{
		AutoDeleteObjects: jsii.Bool(true),
		RemovalPolicy:     awscdk.RemovalPolicy_DESTROY,
		BucketName:        jsii.String(checkEnv("S3_ARTIFACT_BUCKET_NAME")),
		Encryption:        awss3.BucketEncryption_S3_MANAGED,
		BlockPublicAccess: awss3.BlockPublicAccess_BLOCK_ALL(),
		EnforceSSL:        jsii.Bool(true),
		Versioned:         jsii.Bool(true),
	})
}
