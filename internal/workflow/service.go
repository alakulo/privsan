package workflow

import (
	"context"
	"privsan/internal/model"
	"privsan/internal/policy"
	"privsan/internal/scan"
	"privsan/internal/storage"
)

type Service struct{ Policy *policy.Policy }

func (s Service) Scan(ctx context.Context, opts scan.Options, progress func(scan.Progress)) model.Snapshot {
	return scan.Run(ctx, opts, s.Policy, progress)
}

type WriteOptions struct {
	Output, BackupDir string
	InPlace, DryRun   bool
}

func (s Service) Write(ctx context.Context, snapshot *model.Snapshot, opts WriteOptions) model.Operation {
	if opts.DryRun {
		return model.Operation{}
	}
	if !snapshot.Complete {
		return model.Operation{Kind: "write", Issues: []model.Issue{model.Error("E_INPUT", "", "incomplete scan cannot be written")}}
	}
	if opts.InPlace && opts.Output == "" && opts.BackupDir != "" {
		return storage.Apply(ctx, snapshot, opts.BackupDir)
	}
	if !opts.InPlace && opts.Output != "" && opts.BackupDir == "" {
		return storage.Export(ctx, snapshot, opts.Output)
	}
	return model.Operation{Kind: "write", Issues: []model.Issue{model.Error("E_INPUT", "", "choose output or in-place with backup directory")}}
}
