package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/auth"
	"github.com/azazo1/bilibili-cli/internal/config"
	"github.com/azazo1/bilibili-cli/internal/output"
)

var Version = "devel"

type App struct {
	API         *api.Client
	Auth        *auth.Store
	Config      config.Config
	ConfigStore *config.Store
	ConfigErr   error
	Out         *output.Writer
	In          io.Reader
	Logger      *slog.Logger
}

func NewApp() *App {
	logger := slog.Default()
	configStore := config.NewStore()
	settings, configErr := configStore.Load()
	if configErr != nil {
		settings = config.Default()
	}
	client := api.NewClient()
	client.SetTimeout(settings.Network.TimeoutSeconds)
	store := auth.NewStore(client)
	store.Logger = logger
	out := output.NewWriter()
	out.DefaultMode = settings.Output.Format
	return &App{
		API:         client,
		Auth:        store,
		Config:      settings,
		ConfigStore: configStore,
		ConfigErr:   configErr,
		Out:         out,
		In:          os.Stdin,
		Logger:      logger,
	}
}

type ExitError struct {
	Code     int
	Rendered bool
	Err      error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (a *App) ResolveOutput(asJSON, asYAML bool) (output.Mode, error) {
	return a.Out.Resolve(asJSON, asYAML)
}

func (a *App) Fail(err error, action string, mode output.Mode) error {
	return a.fail(err, action, mode, nil)
}

func (a *App) fail(err error, action string, mode output.Mode, details any) error {
	if err == nil {
		err = errors.New(action)
	}
	if apiErr, ok := err.(*api.Error); ok && apiErr.Action == "" {
		copy := *apiErr
		copy.Action = action
		err = &copy
	}
	if writeErr := a.Out.EmitErrorWithDetails(err, action, mode, details); writeErr != nil {
		err = fmt.Errorf("%w; output: %v", err, writeErr)
	}
	return &ExitError{Code: 1, Rendered: true, Err: err}
}

func (a *App) Complete(data any, mode output.Mode, render func(io.Writer)) error {
	if mode != output.ModeRich {
		return a.Out.EmitSuccess(data, mode)
	}
	if render != nil {
		render(a.Out.Stdout)
	}
	return nil
}

func (a *App) CompleteTable(data any, mode output.Mode, asJSON, asYAML bool, render func(io.Writer)) error {
	if !asJSON && !asYAML && mode == output.ModeYAML && a.Out != nil && a.Out.UsesAutoMode() {
		if render != nil {
			render(a.Out.Stdout)
		}
		return nil
	}
	return a.Complete(data, mode, render)
}

func (a *App) Execute(ctx context.Context) error {
	root := NewRoot(a)
	command, err := root.ExecuteContextC(ctx)
	if err == nil {
		return nil
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return err
	}
	if command == nil {
		command = root
	}
	return a.failUsage(command, err)
}

type Access int

const (
	AccessRead Access = iota
	AccessWrite
	AccessDestructive
)

func (a *App) RequireCredential(ctx context.Context, access Access, mode output.Mode, message string) (*api.Credential, error) {
	write := access == AccessWrite || access == AccessDestructive
	if write {
		if err := a.RequireWritable(mode, "账户写操作"); err != nil {
			return nil, err
		}
	}
	if access == AccessDestructive {
		if err := a.RequireNonDestructive(mode, "破坏性操作"); err != nil {
			return nil, err
		}
	}
	requested := auth.ModeRead
	if write {
		requested = auth.ModeWrite
	}
	credential, err := a.Auth.GetCredential(ctx, requested)
	if err != nil {
		return nil, a.Fail(err, "读取登录凭证失败", mode)
	}
	if credential != nil {
		return credential, nil
	}
	if write {
		if saved, _ := a.Auth.GetCredential(ctx, auth.ModeOptional); saved != nil && saved.BiliJct == "" {
			return nil, a.Fail(api.NewError(api.CodePermissionDenied, "", "当前登录凭证不支持写操作, 缺少 bili_jct. 请执行 bili me login 重新登录"), "", mode)
		}
	}
	if message == "" {
		message = "未登录. 使用 bili me login 登录"
	}
	return nil, a.Fail(api.NewError(api.CodeNotAuthenticated, "", message), "", mode)
}

func (a *App) OptionalCredential(ctx context.Context) *api.Credential {
	credential, err := a.Auth.GetCredential(ctx, auth.ModeOptional)
	if err != nil {
		a.Logger.Warn("读取登录凭证失败, 将以未登录状态继续", "error", err)
		return nil
	}
	if credential == nil {
		a.Logger.Warn("未登录, 某些信息可能缺失. 使用 bili me login 登录以获取完整信息")
	}
	return credential
}

func (a *App) RequireWritable(mode output.Mode, action string) error {
	if !a.Config.Safety.ReadOnly {
		return nil
	}
	return a.Fail(api.NewError(api.CodePermissionDenied, "", "只读模式已启用, 禁止 "+action), "", mode)
}

func (a *App) RequireNonDestructive(mode output.Mode, action string) error {
	if !a.Config.Safety.NonDestructive {
		return nil
	}
	return a.Fail(api.NewError(api.CodePermissionDenied, "", "非破坏模式已启用, 禁止 "+action), "", mode)
}

func (a *App) ShouldConfirmDangerousAction() bool {
	return a.Config.Safety.ConfirmDangerousActions
}

func (a *App) setupLogging(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	options := &slog.HandlerOptions{Level: level}
	stderr := io.Writer(os.Stderr)
	if a.Out != nil && a.Out.Stderr != nil {
		stderr = a.Out.Stderr
	}
	a.Logger = slog.New(slog.NewTextHandler(stderr, options))
	slog.SetDefault(a.Logger)
	a.Auth.Logger = a.Logger
	a.API.Logger = a.Logger
}
