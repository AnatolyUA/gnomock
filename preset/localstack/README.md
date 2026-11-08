# Gnomock Localstack

Gnomock Localstack is a [Gnomock](https://github.com/orlangure/gnomock) preset
for running tests against AWS services locally, powered by
[Localstack](https://github.com/localstack/localstack) project. It allows
to setup a number of supported AWS services locally, run tests against
them, and tear them down easily.

See [Localstack](https://github.com/localstack/localstack) documentation for
more details.

### Versions and auth token

The default version is `4.14`, the last LocalStack release that runs without
an auth token. LocalStack 2026.x images need one: pass it with
`localstack.WithAuthToken(token)`, or set the `LOCALSTACK_AUTH_TOKEN`
environment variable, which the preset uses when `WithAuthToken` isn't given.
Images older than 1.3.0 are not supported.

### Testing against local S3

```go
package localstack_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/localstack"
)

func ExamplePreset_s3() {
	p := localstack.Preset(localstack.WithServices(localstack.S3))
	c, _ := gnomock.Start(p)

	defer func() { _ = gnomock.Stop(c) }()

	ctx := context.Background()
	endpoint := fmt.Sprintf("http://%s/", c.Address(localstack.APIPort))
	svc := s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider("a", "b", "c"),
		Region:       "us-east-1",
	})

	_, _ = svc.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String("foo"),
	})

	out, _ := svc.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String("foo"),
	})
	fmt.Println("keys before:", *out.KeyCount)

	_, _ = svc.PutObject(ctx, &s3.PutObjectInput{
		Body:   bytes.NewReader([]byte("this is a file")),
		Key:    aws.String("file"),
		Bucket: aws.String("foo"),
	})

	out, _ = svc.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String("foo"),
	})
	fmt.Println("keys after:", *out.KeyCount)

	// Output:
	// keys before: 0
	// keys after: 1
}
```

### Testing against local SQS+SNS

```go
package localstack_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/localstack"
)

func ExamplePreset_sqs_sns() {
	p := localstack.Preset(
		localstack.WithServices(localstack.SNS, localstack.SQS),
	)
	c, _ := gnomock.Start(p)

	defer func() { _ = gnomock.Stop(c) }()

	ctx := context.Background()
	endpoint := fmt.Sprintf("http://%s", c.Address(localstack.APIPort))
	creds := credentials.NewStaticCredentialsProvider("a", "b", "c")

	sqsService := sqs.New(sqs.Options{
		BaseEndpoint: aws.String(endpoint),
		Credentials:  creds,
		Region:       "us-east-1",
	})
	snsService := sns.New(sns.Options{
		BaseEndpoint: aws.String(endpoint),
		Credentials:  creds,
		Region:       "us-east-1",
	})

	queue, _ := sqsService.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String("my_queue"),
	})

	attrs, _ := sqsService.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       queue.QueueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	queueARN := attrs.Attributes["QueueArn"]

	topic, _ := snsService.CreateTopic(ctx, &sns.CreateTopicInput{
		Name: aws.String("my_topic"),
	})

	_, _ = snsService.Subscribe(ctx, &sns.SubscribeInput{
		Protocol: aws.String("sqs"),
		Endpoint: aws.String(queueARN),
		TopicArn: topic.TopicArn,
	})

	_, _ = snsService.Publish(ctx, &sns.PublishInput{
		TopicArn: topic.TopicArn,
		Message:  aws.String("foobar"),
	})

	messages, _ := sqsService.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:        queue.QueueUrl,
		WaitTimeSeconds: 1,
	})
	fmt.Println("messages:", len(messages.Messages))

	var msg map[string]string

	_ = json.Unmarshal([]byte(*messages.Messages[0].Body), &msg)
	fmt.Println("message:", msg["Message"])

	// Output:
	// messages: 1
	// message: foobar
}
```
