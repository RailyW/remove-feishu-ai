package feature

import "context"

type Tx interface {
	BackupFile(relativePath, sourcePath string) error
}

type Env interface {
	InstallPath() string
}

type Feature interface {
	ID() string
	DisplayName() string
	Detect(ctx context.Context, env Env) (State, error)
	Remove(ctx context.Context, env Env, tx Tx) error
	Restore(ctx context.Context, env Env, tx Tx) error
}
