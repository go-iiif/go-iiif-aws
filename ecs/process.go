package ecs

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	aws_ecs "github.com/aws/aws-sdk-go/service/ecs"
	"github.com/whosonfirst/go-whosonfirst-aws/session"
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
