# Errors Encountered:


## Error 1:
```
Failed resources:
CodePipelineCdkStack | 9:15:20 PM | CREATE_FAILED        | AWS::CodeBuild::Project     | CodeBuildV1 (CodeBuildV1A60ECD8B) Failed to call CreateWebhook, reason: Access token not found in CodeBuild project for server type github (Service: AWSCodeBuild; Status Code: 400; Error Code: ResourceNotFoundException; Request ID: 8e98045e-e5af-4025-88ef-dcf995e585be; Proxy: null)
❌  CodePipelineCdkStack failed: _ToolkitError: The stack named CodePipelineCdkStack failed creation, it may need to be manually deleted from the AWS console: ROLLBACK_COMPLETE: Failed to call CreateWebhook, reason: Access token not found in CodeBuild project for server type github (Service: AWSCodeBuild; Status Code: 400; Error Code: ResourceNotFoundException; Request ID: 8e98045e-e5af-4025-88ef-dcf995e585be; Proxy: null)

-> Turns out the root of cause of this is the lack of IAM role/policy. Even though the PAT is valid and properly configured for both PlainText and SecretsManager, my CodeBuild does not have the right permissions to create webhooks in my GitHub repo. 

```

## Error 2:
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