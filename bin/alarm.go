package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

func alarm(stack constructs.Construct, name string, metric awscloudwatch.Metric) awscloudwatch.Alarm {
	alarm := awscloudwatch.NewAlarm(stack, &name, &awscloudwatch.AlarmProps{
		AlarmName:          &name,
		Metric:             metric,
		Threshold:          jsii.Number(1),
		EvaluationPeriods:  jsii.Number(1),
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD,
		TreatMissingData:   awscloudwatch.TreatMissingData_IGNORE,
	})

	return alarm
}
