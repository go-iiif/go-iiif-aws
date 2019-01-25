package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	aws_events "github.com/aws/aws-lambda-go/events"
	aws_lambda "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/whosonfirst/go-whosonfirst-aws/lambda"
	"github.com/whosonfirst/go-whosonfirst-aws/session"
	"github.com/whosonfirst/go-whosonfirst-cli/flags"
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

	svc := ecs.New(sess)

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

	network := &ecs.NetworkConfiguration{
		AwsvpcConfiguration: &ecs.AwsVpcConfiguration{
			AssignPublicIp: public_ip,
			SecurityGroups: security_groups,
			Subnets:        subnets,
		},
	}

	process_override := &ecs.ContainerOverride{
		Name:    aws.String(opts.Container),
		Command: cmd,
	}

	overrides := &ecs.TaskOverride{
		ContainerOverrides: []*ecs.ContainerOverride{process_override},
	}

	input := &ecs.RunTaskInput{
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

	task_id := rsp.Tasks[0].TaskArn

	if opts.Wait {

		tasks := []*string{
			task_id,
		}

		pending := &ecs.DescribeTasksInput{
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

func main() {

	var ecs_dsn = flag.String("ecs-dsn", "", "A valid (go-whosonfirst-aws) ECS DSN.")

	var container = flag.String("container", "", "The name of your AWS ECS container.")
	var cluster = flag.String("cluster", "", "The name of your AWS ECS cluster.")
	var task = flag.String("task", "", "The name of your AWS ECS task (inclusive of its version number),")

	var config = flag.String("config", "/etc/go-iiif/config.json", "The path your IIIF config (on/in your container).")
	var instructions = flag.String("instructions", "/etc/go-iiif/instructions.json", "The path your IIIF processing instructions (on/in your container).")

	var strip_paths = flag.Bool("strip-paths", true, "Strip directory tree from URIs.")
	var wait = flag.Bool("wait", false, "Wait for the task to complete.")

	var mode = flag.String("mode", "task", "Valid modes are: lambda (run as a Lambda function), invoke (invoke this Lambda function), task (run this ECS task).")

	var lambda_dsn = flag.String("lambda-dsn", "", "A valid (go-whosonfirst-aws) Lambda DSN. Required if -mode is \"invoke\".")
	var lambda_func = flag.String("lambda-func", "", "A valid Lambda function name. Required if -mode is \"invoke\".")
	var lambda_type = flag.String("lambda-type", "", "A valid go-aws-sdk lambda.InvocationType string. Required if -mode is \"invoke\".")

	var subnets flags.MultiString
	flag.Var(&subnets, "subnet", "One or more AWS subnets in which your task will run.")

	var security_groups flags.MultiString
	flag.Var(&security_groups, "security-group", "One of more AWS security groups your task will assume.")

	var uris flags.MultiString
	flag.Var(&uris, "uri", "One or more valid IIIF URIs.")

	flag.Parse()

	err := flags.SetFlagsFromEnvVars("IIIF_PROCESS")

	if err != nil {
		log.Fatal(err)
	}

	if *mode == "lambda" {

		if *wait == true {
			log.Println("[WARNING] -wait flag when running as a Lambda function seems to always time out, because... computers?")
		}

		expand := func(candidates []string, sep string) []string {

			expanded := make([]string, 0)

			for _, c := range candidates {

				for _, v := range strings.Split(c, sep) {
					expanded = append(expanded, v)
				}
			}

			return expanded
		}

		uris = expand(uris, ",")
		subnets = expand(subnets, ",")
		security_groups = expand(security_groups, ",")
	}

	opts := &ProcessTaskOptions{
		DSN:            *ecs_dsn,
		Task:           *task,
		Wait:           *wait,
		Container:      *container,
		Cluster:        *cluster,
		Subnets:        subnets,
		SecurityGroups: security_groups,
		URIs:           uris,
		Config:         *config,
		Instructions:   *instructions,
		StripPaths:     *strip_paths,
	}

	switch *mode {

	case "lambda":

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

		aws_lambda.Start(handler)

	case "invoke":

		// https://github.com/aws/aws-lambda-go/blob/master/events/s3.go

		svc, err := lambda.NewLambdaServiceWithDSN(*lambda_dsn)

		if err != nil {
			log.Fatal(err)
		}

		s3_records := make([]aws_events.S3EventRecord, len(uris))

		for i, u := range uris {

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

		// THIS NEEDS BETTER RESPONSE WAH-WAH
		// https://docs.aws.amazon.com/sdk-for-go/api/service/lambda/#InvokeOutput

		err = lambda.InvokeFunction(svc, *lambda_func, *lambda_type, s3_event)

		if err != nil {
			log.Fatal(err)
		}

	case "task":

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rsp, err := LaunchProcessTask(ctx, opts)

		if err != nil {
			log.Fatal(err)
		}

		log.Println(rsp)

	default:

		log.Fatal("unknown task")
	}
}
