package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/feature/user/repository"
	"github.com/jarviisha/darkvoid/internal/feature/user/service"
	"github.com/jarviisha/darkvoid/pkg/config"
	"github.com/jarviisha/darkvoid/pkg/database"
)

// passwordEnv lets automation pass a password without exposing it on the
// command line (argv is world-readable via /proc and lands in shell history).
//
//nolint:gosec // G101: this is the env var *name*, not a credential value.
const passwordEnv = "DARKVOIDCTL_PASSWORD"

// readPassword resolves a password without ever taking it as a flag:
//  1. the DARKVOIDCTL_PASSWORD env var (for scripted/automated use), else
//  2. an interactive prompt with echo disabled (a real terminal), else
//  3. a single line piped on stdin.
func readPassword(prompt string) (string, error) {
	if pw := os.Getenv(passwordEnv); pw != "" {
		return pw, nil
	}
	fd := int(os.Stdin.Fd()) //nolint:gosec // G115: a process file descriptor is a small non-negative int.
	if term.IsTerminal(fd) {
		fmt.Fprintf(os.Stderr, "%s: ", prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(b), nil
	}
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return strings.TrimRight(sc.Text(), "\r\n"), nil
	}
	return "", fmt.Errorf("no password provided (set %s, pipe on stdin, or run interactively)", passwordEnv)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "user":
		runUser(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `darkvoidctl — DarkVoid operator CLI

Usage:
  darkvoidctl user reset-password -username <u>
  darkvoidctl user create -username <u> -email <e> [-display-name <n>]
  darkvoidctl user list [-q <query>] [-limit <n>] [-active-only]
  darkvoidctl user grant-role  -username <u> -role <r>
  darkvoidctl user revoke-role -username <u> -role <r>
  darkvoidctl user roles                      list the assignable roles
  darkvoidctl user deactivate -username <u>

Passwords are never taken as flags: reset-password and create prompt
interactively (echo off), or read `+passwordEnv+` / a piped stdin line.

Reads DB settings from the environment / .env (same as the API server).
`)
}

// deps bundles what the user subcommands need. openDeps wires them from config.
type deps struct {
	pool     *pgxpool.Pool
	userRepo *repository.UserRepository
	roleRepo *repository.RoleRepository
	userSvc  *service.UserService
}

func openDeps(ctx context.Context) (*deps, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	pool, err := database.NewPostgresPool(ctx, &database.Config{
		Host:            cfg.Database.Host,
		Port:            cfg.Database.Port,
		User:            cfg.Database.User,
		Password:        cfg.Database.Password,
		Database:        cfg.Database.Database,
		SSLMode:         cfg.Database.SSLMode,
		MaxConns:        cfg.Database.MaxConns,
		MinConns:        cfg.Database.MinConns,
		MaxConnLifetime: cfg.Database.MaxConnLifetime,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("connect db: %w", err)
	}
	if err := database.HealthCheck(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("db health check: %w", err)
	}

	userRepo := repository.NewUserRepository(pool)
	d := &deps{
		pool:     pool,
		userRepo: userRepo,
		roleRepo: repository.NewRoleRepository(pool),
		// Avatar/cover storage is nil: no CLI command touches it.
		userSvc: service.NewUserService(userRepo, nil),
	}
	return d, func() { pool.Close() }, nil
}

func runUser(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}

	action := args[0]
	rest := args[1:]

	var err error
	switch action {
	case "reset-password":
		err = cmdResetPassword(rest)
	case "create":
		err = cmdCreate(rest)
	case "list":
		err = cmdList(rest)
	case "grant-role":
		err = cmdSetRole(rest, true)
	case "revoke-role":
		err = cmdSetRole(rest, false)
	case "roles":
		err = cmdRoles()
	case "deactivate":
		err = cmdDeactivate(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown user action %q\n\n", action)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// resolveUser looks up a user by username and returns a friendly error when missing.
func resolveUser(ctx context.Context, d *deps, username string) (*entity.User, error) {
	u, err := d.userSvc.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("lookup user %q: %w", username, err)
	}
	return u, nil
}

func cmdResetPassword(args []string) error {
	fs := flag.NewFlagSet("user reset-password", flag.ExitOnError)
	username := fs.String("username", "", "username of the account (required)")
	_ = fs.Parse(args)
	if *username == "" {
		return fmt.Errorf("-username is required")
	}
	password, err := readPassword("New password")
	if err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("password must not be empty")
	}

	ctx := context.Background()
	d, cleanup, err := openDeps(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	u, err := resolveUser(ctx, d, *username)
	if err != nil {
		return err
	}
	if err := d.userSvc.AdminResetPassword(ctx, u.ID, password); err != nil {
		return err
	}
	fmt.Printf("password reset for %s (%s)\n", u.Username, u.ID)
	return nil
}

func cmdCreate(args []string) error {
	fs := flag.NewFlagSet("user create", flag.ExitOnError)
	username := fs.String("username", "", "username (required)")
	email := fs.String("email", "", "email (required)")
	displayName := fs.String("display-name", "", "display name (defaults to username)")
	_ = fs.Parse(args)
	if *username == "" || *email == "" {
		return fmt.Errorf("-username and -email are required")
	}
	password, err := readPassword("Password")
	if err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("password must not be empty")
	}
	dn := *displayName
	if dn == "" {
		dn = *username
	}

	ctx := context.Background()
	d, cleanup, err := openDeps(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	id, err := d.userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username:    *username,
		Email:       *email,
		DisplayName: dn,
		Password:    password,
	})
	if err != nil {
		return err
	}
	fmt.Printf("created user %s (%s)\n", *username, id)
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("user list", flag.ExitOnError)
	query := fs.String("q", "", "filter by username/email substring")
	limit := fs.Int("limit", 50, "max rows")
	activeOnly := fs.Bool("active-only", false, "only active accounts")
	_ = fs.Parse(args)

	ctx := context.Background()
	d, cleanup, err := openDeps(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	var q *string
	if *query != "" {
		q = query
	}
	var active *bool
	if *activeOnly {
		t := true
		active = &t
	}

	users, err := d.userRepo.AdminListUsers(ctx, q, active, int32(*limit), 0) //nolint:gosec // limit is small operator input
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	// tabwriter buffers; a write error only surfaces on Flush, checked below.
	_, _ = fmt.Fprintln(w, "USERNAME\tEMAIL\tACTIVE\tCREATED\tID")
	for _, u := range users {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\n",
			u.Username, u.Email, u.IsActive, u.CreatedAt.Format(time.DateOnly), u.ID)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\n%d user(s)\n", len(users))
	return nil
}

// cmdSetRole grants or revokes any role the application recognises. It replaces the
// earlier admin-only promote/demote pair, which could not reach the bot role the
// content bot's runner account needs.
func cmdSetRole(args []string, grant bool) error {
	name := "user revoke-role"
	if grant {
		name = "user grant-role"
	}
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	username := fs.String("username", "", "username of the account (required)")
	roleName := fs.String("role", "", "role to change: "+knownRoles()+" (required)")
	_ = fs.Parse(args)
	if *username == "" {
		return fmt.Errorf("-username is required")
	}
	if *roleName == "" {
		return fmt.Errorf("-role is required (one of %s)", knownRoles())
	}

	// Reject an unknown name here so it reads as a typo rather than surfacing as an
	// opaque CHECK-constraint violation from the database.
	role, ok := entity.ParseRole(*roleName)
	if !ok {
		return fmt.Errorf("unknown role %q (expected one of %s)", *roleName, knownRoles())
	}

	ctx := context.Background()
	d, cleanup, err := openDeps(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	u, err := resolveUser(ctx, d, *username)
	if err != nil {
		return err
	}

	// Both operations are idempotent, so re-granting a role the user already holds or
	// revoking one they never had succeeds without a prior membership check.
	if grant {
		// AssignedBy is nil to mark the grant as made by the system rather than an admin.
		if err := d.roleRepo.AssignRole(ctx, u.ID, role, nil); err != nil {
			return fmt.Errorf("assign %s role: %w", role, err)
		}
		fmt.Printf("granted %s to %s (%s)\n", role, u.Username, u.ID)
		return nil
	}
	if err := d.roleRepo.RemoveRole(ctx, u.ID, role); err != nil {
		return fmt.Errorf("remove %s role: %w", role, err)
	}
	fmt.Printf("revoked %s from %s (%s)\n", role, u.Username, u.ID)
	return nil
}

// cmdRoles lists the assignable roles. The set is fixed at compile time, so this
// never touches the database — it exists so an operator does not have to read
// entity/role.go to find a valid -role value.
func cmdRoles() error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, r := range entity.AllRoles {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", r, r.Description()); err != nil {
			return err
		}
	}
	return w.Flush()
}

// knownRoles renders entity.AllRoles for flag help and error messages, so adding a
// role never leaves the CLI documenting a stale set.
func knownRoles() string {
	names := make([]string, 0, len(entity.AllRoles))
	for _, r := range entity.AllRoles {
		names = append(names, r.String())
	}
	return strings.Join(names, ", ")
}

func cmdDeactivate(args []string) error {
	fs := flag.NewFlagSet("user deactivate", flag.ExitOnError)
	username := fs.String("username", "", "username of the account (required)")
	_ = fs.Parse(args)
	if *username == "" {
		return fmt.Errorf("-username is required")
	}

	ctx := context.Background()
	d, cleanup, err := openDeps(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	u, err := resolveUser(ctx, d, *username)
	if err != nil {
		return err
	}
	if err := d.userSvc.DeactivateUser(ctx, u.ID, nil); err != nil {
		return err
	}
	fmt.Printf("deactivated %s (%s)\n", u.Username, u.ID)
	return nil
}
