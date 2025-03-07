package read

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"terraform-provider-iterative/task"
	"terraform-provider-iterative/task/common"
)

type status string

const (
	statusQueued    status = "queued"
	statusSucceeded status = "succeeded"
	statusFailed    status = "failed"
	statusRunning   status = "running"
)

type Options struct {
	Parallelism int
	Timestamps  bool
	Follow      bool
}

func New(cloud *common.Cloud) *cobra.Command {
	o := Options{}

	cmd := &cobra.Command{
		Use:   "read <name>",
		Short: "Read information from an existing task",
		Long:  ``,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run(cmd, args, cloud)
		},
	}

	cmd.Flags().IntVar(&o.Parallelism, "parallelism", 1, "parallelism")
	cmd.Flags().BoolVar(&o.Timestamps, "timestamps", false, "display timestamps")
	cmd.Flags().BoolVar(&o.Follow, "follow", false, "follow logs")

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, args []string, cloud *common.Cloud) error {
	cfg := common.Task{
		Environment: common.Environment{
			Image: "ubuntu",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), cloud.Timeouts.Read)

	id, err := common.ParseIdentifier(args[0])
	if err != nil {
		return err
	}

	tsk, err := task.New(ctx, *cloud, id, cfg)
	if err != nil {
		return err
	}

        var last int
	for {
		if err := tsk.Read(ctx); err != nil {
			return err
		}

		logs, err := o.getLogs(ctx, tsk)
		if err != nil {
			return err
		}

		status, err := o.getStatus(ctx, tsk)
		if err != nil {
			return err
		}

		if delta := strings.Join(logs[last:], "\n"); delta != "" {
			fmt.Println(delta)
			last = len(logs)
		}

		switch o.Follow {
		case true:
			// disable debug logs for subsequent iterations
			logrus.SetLevel(logrus.WarnLevel)
			// create a new context to reset timeout on every iteration
			ctx, cancel = context.WithTimeout(context.Background(), cloud.Timeouts.Read)
			defer cancel()
		case false:
			return nil
		}

		switch status {
		case statusSucceeded:
			os.Exit(0)
		case statusFailed:
			os.Exit(1)
		default:
			time.Sleep(3 * time.Second)
		}
	}
}

func (o *Options) getLogs(ctx context.Context, tsk task.Task) ([]string, error) {
	logs, err := tsk.Logs(ctx)
	if err != nil {
		return nil, err
	}

	var result []string

	for _, log := range logs {
		for _, line := range strings.Split(strings.Trim(log, "\n"), "\n") {
			if !o.Timestamps {
				_, line, _ = strings.Cut(line, " ")
			}
			result = append(result, line)
		}
	}

	return result, nil
}

func (o *Options) getStatus(ctx context.Context, tsk task.Task) (status, error) {
	for _, event := range tsk.Events(ctx) {
		line := fmt.Sprintf("%s: %s", event.Code, strings.Join(event.Description, " "))
		if o.Timestamps {
			line = fmt.Sprintf("%s %s", event.Time.Format("2006-01-02T15:04:05Z"), line)
		}

		logrus.Debug(line)
	}

	status, err := tsk.Status(ctx)
	if err != nil {
		return "", err
	}

	result := statusQueued

	if status[common.StatusCodeSucceeded] >= o.Parallelism {
		result = statusSucceeded
	}
	if status[common.StatusCodeFailed] > 0 {
		result = statusFailed
	}
	if status[common.StatusCodeActive] >= o.Parallelism {
		result = statusRunning
	}

	logrus.Debug(result)
	return result, nil
}
