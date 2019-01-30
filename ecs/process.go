package ecs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	aws_events "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	aws_ecs "github.com/aws/aws-sdk-go/service/ecs"
	"github.com/whosonfirst/go-whosonfirst-aws/lambda"
	"github.com/whosonfirst/go-whosonfirst-aws/session"
	"log"
	"mime"
	"path/filepath"
	"strings"
)

type ProcessTaskOptions struct {
	DSN            string
	Task           string
	Wait           bool
	Cluster        string
	Container      string
	SecurityGroups []string
	Subnets        []string
	Config         string
	Instructions   string
	URIs           []string
	StripPaths     bool
}

type ProcessTaskResponse struct {
	TaskId string
	URIs   []string
}

func (t *ProcessTaskResponse) String() string {
	return t.TaskId
}

func LaunchProcessTask(ctx context.Context, opts *ProcessTaskOptions) (*ProcessTaskResponse, error) {

	sess, err := session.NewSessionWithDSN(opts.DSN)

	if err != nil {
		return nil, err
	}

	cmd := []*string{
		aws.String("/bin/iiif-process"),
		aws.String("-config"),
		aws.String(opts.Config),
		aws.String("-instructions"),
		aws.String(opts.Instructions),
	}

	images := make([]string, 0)

	for _, im := range opts.URIs {

		if opts.StripPaths {
			im = filepath.Base(im)
		}

		im_ext := filepath.Ext(im)
		im_type := mime.TypeByExtension(im_ext)

		if !strings.HasPrefix(im_type, "image/") {
			msg := fmt.Sprintf("%s has unknown or invalid mime-type %s", im, im_type)
			return nil, errors.New(msg)
		}

		images = append(images, im)
	}

	if len(images) == 0 {
		return nil, errors.New("No images to process")
	}

	for _, im := range images {
		cmd = append(cmd, aws.String("-uri"))
		cmd = append(cmd, aws.String(im))
	}

	// at this point there's nothing IIIF specific about anything
	// that follows - it's pretty much boilerplate AWS ECS invoking
	// code

	svc := aws_ecs.New(sess)

	cluster := aws.String(opts.Cluster)
	task := aws.String(opts.Task)

	launch_type := aws.String("FARGATE")
	public_ip := aws.String("ENABLED")

	subnets := make([]*string, len(opts.Subnets))
	security_groups := make([]*string, len(opts.SecurityGroups))

	for i, sn := range opts.Subnets {
		subnets[i] = aws.String(sn)
	}

	for i, sg := range opts.SecurityGroups {
		security_groups[i] = aws.String(sg)
	}

	network := &aws_ecs.NetworkConfiguration{
		AwsvpcConfiguration: &aws_ecs.AwsVpcConfiguration{
			AssignPublicIp: public_ip,
			SecurityGroups: security_groups,
			Subnets:        subnets,
		},
	}

	process_override := &aws_ecs.ContainerOverride{
		Name:    aws.String(opts.Container),
		Command: cmd,
	}

	overrides := &aws_ecs.TaskOverride{
		ContainerOverrides: []*aws_ecs.ContainerOverride{process_override},
	}

	input := &aws_ecs.RunTaskInput{
		Cluster:              cluster,
		TaskDefinition:       task,
		LaunchType:           launch_type,
		NetworkConfiguration: network,
		Overrides:            overrides,
	}

	rsp, err := svc.RunTask(input)

	if err != nil {
		return nil, err
	}

	// https://github.com/buildkite/ecs-run-task/blob/master/runner/runner.go#L148-L208
	// this appears to be how you capture the output of an ECS task?
	// (20190124/thisisaaronland)

	task_id := rsp.Tasks[0].TaskArn

	if opts.Wait {

		tasks := []*string{
			task_id,
		}

		pending := &aws_ecs.DescribeTasksInput{
			Cluster: cluster,
			Tasks:   tasks,
		}

		err = svc.WaitUntilTasksStopped(pending)

		if err != nil {
			return nil, err
		}
	}

	task_rsp := ProcessTaskResponse{
		TaskId: *task_id,
		URIs:   opts.URIs,
	}

	return &task_rsp, nil
}

func InvokeLambdaHandlerFunc(opts *ProcessTaskOptions, lambda_dsn string, lambda_func string, lambda_type string) (interface{}, error) {

	// https://github.com/aws/aws-lambda-go/blob/master/events/s3.go

	svc, err := lambda.NewLambdaServiceWithDSN(lambda_dsn)

	if err != nil {
		return nil, err
	}

	s3_records := make([]aws_events.S3EventRecord, len(opts.URIs))

	for i, u := range opts.URIs {

		s3_object := aws_events.S3Object{
			Key: u,
		}

		s3_entity := aws_events.S3Entity{
			Object: s3_object,
		}

		s3_records[i] = aws_events.S3EventRecord{
			S3: s3_entity,
		}
	}

	s3_event := aws_events.S3Event{
		Records: s3_records,
	}

	rsp, err := lambda.InvokeFunction(svc, lambda_func, lambda_type, s3_event)

	if err != nil {
		return nil, err
	}

	return rsp, nil
}

func LambdaHandlerFunc(opts *ProcessTaskOptions) func(ctx context.Context, ev aws_events.S3Event) (*ProcessTaskResponse, error) {

	handler := func(ctx context.Context, ev aws_events.S3Event) (*ProcessTaskResponse, error) {

		uris := make([]string, 0)

		for _, r := range ev.Records {

			s3_entity := r.S3
			s3_obj := s3_entity.Object
			s3_key := s3_obj.Key

			im_ext := filepath.Ext(s3_key)
			im_type := mime.TypeByExtension(im_ext)

			if !strings.HasPrefix(im_type, "image/") {
				continue
			}

			uris = append(uris, s3_key)
		}

		if len(uris) == 0 {
			return nil, nil
		}

		opts.URIs = uris

		rsp, err := LaunchProcessTask(ctx, opts)

		if err != nil {
			return nil, err
		}

		enc_rsp, err := json.Marshal(rsp)

		if err != nil {
			return nil, err
		}

		log.Println(string(enc_rsp))

		return rsp, nil
	}

	return handler
}
