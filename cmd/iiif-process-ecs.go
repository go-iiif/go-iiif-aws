package main

import (
	"context"
	"flag"
	"github.com/aaronland/go-iiif-aws/ecs"
	aws_lambda "github.com/aws/aws-lambda-go/lambda"
	"github.com/whosonfirst/go-whosonfirst-cli/flags"
	"log"
	"strings"
)

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

	opts := &ecs.ProcessTaskOptions{
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

		handler := ecs.LambdaHandlerFunc(opts)
		aws_lambda.Start(handler)

	case "invoke":

		_, err := ecs.InvokeLambdaHandlerFunc(opts, *lambda_dsn, *lambda_func, *lambda_type)

		if err != nil {
			log.Fatal(err)
		}

	case "task":

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rsp, err := ecs.LaunchProcessTask(ctx, opts)

		if err != nil {
			log.Fatal(err)
		}

		log.Println(rsp)

	default:
		log.Fatal("unknown task")
	}
}
