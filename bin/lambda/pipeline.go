package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"
)

// Response structure
type Response struct {
	Message string `json:"message"`
}

// Handler function for AWS Lambda
func handler(ctx context.Context) (string, error) {
	return "Lambda deployment successful", nil
}

func main() {
	lambda.Start(handler)
}
