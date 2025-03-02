package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awss3"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssns"
)

type PipelineResources struct {
	stack          awscdk.Stack
	githubSecret   awssecretsmanager.ISecret
	artifactBucket awss3.IBucket
	alarmTopic     awssns.ITopic
}
