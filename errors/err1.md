# Errors Encountered:

## Error 1:
```
➜ cdk synth
panic: Error: Resolution error: Resolution error: Resolution error: Resolution error: Synthing a secret value to Resources/${Token[CodePipelineCdkStack.CodeBuildRole.DefaultPolicy.Resource.LogicalID.30]}/Properties/policyDocument/Statement/1/Resource. Using a SecretValue here risks exposing your secret. Only pass SecretValues to constructs that accept a SecretValue property, or call AWS Secrets Manager directly in your runtime code. Call 'secretValue.unsafeUnwrap()' if you understand and accept the risks..
	Object creation stack:
	  at stack traces disabled.
	Object creation stack:
	  at stack traces disabled..

goroutine 1 [running]:
github.com/aws/jsii-runtime-go/runtime.Invoke({0x1036a9000, 0x140001042b0}, {0x10311595a, 0x5}, {0x140002b5e30, 0x1, 0x1}, {0x10354ce80, 0x140003463e0})
	/Users/victorraeva/go/pkg/mod/github.com/aws/jsii-runtime-go@v1.106.0/runtime/runtime.go:229 +0x1ac
github.com/aws/aws-cdk-go/awscdk/v2.(*jsiiProxy_App).Synth(0x140001042b0, 0x0)
	/Users/victorraeva/go/pkg/mod/github.com/aws/aws-cdk-go/awscdk/v2@v2.178.2/App.go:322 +0x84
main.main()
	/Volumes/r3/course/awsCloud-r3/project-r3/aws/cdk/bin/cdk.go:192 +0x114
exit status 2

-> Start by creating a GitHub token. Select the repo & admin:repo_hook permissions 
-> from the aws console or cli, (console recommended). Store the secret using plain text, no json formatting.
-> I also disabled webhook for CodeBuild. CodePipeline now references it instead.

Then this: 
-> // Secret Manager definition
	// token, here is my secret name in SecretsManager
	githubSecret := awssecretsmanager.Secret_FromSecretNameV2(stack, jsii.String("GitHubTokenSecret"), jsii.String("token"))
	oauthTokenSecret := githubSecret.SecretValue() // I've also referenced it here. This will be passed to CodePipeline GitHubSourceAction
	While the SecretARN of the githubSecret will be passed to CodeBuild && Lambda (if needed), like this:
	-> githubSecret.SecretARN()

```

## Error 2:
```
From GitHubActions Workflow:

Run cdk deploy --app "go run bin/cdk.go" --require-approval never
  
[Warning at /CodePipelineCdkStack/pipelineV1] V1 pipeline type is implicitly selected when `pipelineType` is not set. If you want to use V2 type, set `PipelineType.V2`. [ack: @aws-cdk/aws-codepipeline:unspecifiedPipelineType]
✨  Synthesis time: 1.97s
current credentials could not be used to assume 'arn:aws:iam::***:role/cdk-hnb659fds-deploy-role-***-us-east-1', but are for the right account. Proceeding anyway.
CodePipelineCdkStack: This CDK deployment requires bootstrap stack version '6', but during the confirmation via SSM parameter /cdk-bootstrap/hnb659fds/version the following error occurred: AccessDeniedException: User: arn:aws:sts::***:assumed-role/GitHubActionsCICD/GitHubActions is not authorized to perform: ssm:GetParameter on resource: arn:aws:ssm:us-east-1:***:parameter/cdk-bootstrap/hnb659fds/version because no identity-based policy allows the ssm:GetParameter action
Error: Process completed with exit code 1.

--> arn:aws:sts::***:assumed-role/GitHubActionsCICD/GitHubActions needs the right permissions for cdk deploy: 
--> ssm:GetParameter needs to be assigned to GitHubActions
```

## Error 3:
```
➜ cdk deploy
CodePipelineCdkStack: deploying... [1/1]
CodePipelineCdkStack: creating CloudFormation changeset...
❌  CodePipelineCdkStack failed: ValidationError: Circular dependency between resources: [LambdaDeploymentAlarm2660C436, pipelineHandlerInvokeaogYRCynkngS5YZgSc9k7awYl6Ivokue2Lb6NvKr0R040AC3EBA, pipelineHandlerCurrentVersionB47122C3e411c75096b2187356d7da4663a671ce, pipelineHandlerFunctionUrl1185A30B, pipelineHandlerEventInvokeConfig2FBD08A6, production98A682B9, BlueGreenDeployment5C188134, pipelineHandlerB6D81FA3, pipelineHandlerServiceRoleDefaultPolicyCB643587]

--> use case: was using cloudwatch alarms + codedeploy for BG deployment, and then attaching this deploymentGroupV1 which in turn depends on the 
	lambdaFunctionV1. 
--> Two options: 
	1, custom Lambda function to handle validation checks after a new version of lambda is deployed. This new custom Lambda function can monitor cloudwatch metrics and if validation checks fail this can trigger a rollback by invoking codedeploy API. 
	2, Use codebuild in-built health checks for now. Its simpler and mostly sufficient, unless granular permissions are needed. 
	3, Modify the CloudWatch Alarm logic by removing it from the direct deploymentGroupV1 definition. Instead of attaching the alarm inside deploymentGroupV1, define it later in a separate construct: 
		awscdk.NewCfnOutput(stack, jsii.String("RollbackAlarmOutput"), &awscdk.CfnOutputProps{
    	Value: rollbackAlarm.AlarmArn(),
})
```

## Error 4:
```
➜ cdk deploy
Failed resources:
CodePipelineCdkStack | 1:15:05 PM | CREATE_FAILED        | AWS::IAM::Role                   | LambdaExecRole (LambdaExecRoleA88165BD) Resource handler returned message: "Policy arn:aws:iam::aws:policy/AWSLambdaBasicExecutionRole does not exist or is not attachable. (Service: Iam, Status Code: 404, Request ID: 47e2d22f-38ad-490b-8075-3da47be02387)" (RequestToken: a8674483-8afc-0735-6e09-ea5ee437bfda, HandlerErrorCode: NotFound)
❌  CodePipelineCdkStack failed: _ToolkitError: The stack named CodePipelineCdkStack failed creation, it may need to be manually deleted from the AWS console: ROLLBACK_COMPLETE: Resource handler returned message: "Policy arn:aws:iam::aws:policy/AWSLambdaBasicExecutionRole does not exist or is not attachable. (Service: Iam, Status Code: 404, Request ID: 47e2d22f-38ad-490b-8075-3da47be02387)" (RequestToken: a8674483-8afc-0735-6e09-ea5ee437bfda, HandlerErrorCode: NotFound)

--> Remove the IAM service/AWSLambdaBasicExecutionRole or /AWSLambdaBasicExecutionRole role from your Lambda function

```

```
➜ cdk bootstrap
panic: Invalid S3 bucket name (value: cdk-hnb659fds-assets--)
	Bucket name must end with an uppercase, lowercase character or number

	--> check if the bucket is used. Since S3 buckets are global, they can't be used by anyone else.
	--> run: aws s3 ls
	--> delete any duplicate buckets --> use this script from the terminal or delete from AWS console: 
		for bucket in $(aws s3 ls | awk '{print $3}'); do
  			aws s3 rb s3://$bucket --force
		done

		Remember to run: cdk bootstrap (as the initial bucket for your cdk will be deleted)

	--> The core issue has to do with either the AWS region or account ID. If not specified or if you are using .env, make sure the variables are declared and used properly. 

	--> You can also use a custom synthensizer, this helps: ([custom synth](https://docs.aws.amazon.com/cdk/v2/guide/bootstrapping-troubleshoot.html))
```

````
CodePipelineCdkStack: fail: No bucket named 'cdk-hnb659fds-assets-445567116635-us-east-1'. Is account 445567116635 bootstrapped?

--> This occurs if the CDK was bootstrapped with the wrong AWS region. You can resolve it, with the folllowing:
--> Delete the CDKToolKit: aws cloudformation delete-stack --stack-name CDKToolkit
--> Rerun the bootstrp: cdk bootstrap aws://4----------5/us-east-1
```