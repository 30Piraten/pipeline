package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssns"
	"github.com/aws/jsii-runtime-go"
)

// Monitoring resources
func createMonitoringResources(stack awscdk.Stack) awssns.ITopic {
	return awssns.NewTopic(stack, jsii.String("PipelineAlarmTopic"), &awssns.TopicProps{
		TopicName:   jsii.String("pipeline-alarms"),
		DisplayName: jsii.String("Pipeline Alarms"),
	})
}
