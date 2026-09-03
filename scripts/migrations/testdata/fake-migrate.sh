#!/bin/sh

set -eu

: "${FAKE_MIGRATION_STATE:?FAKE_MIGRATION_STATE is required}"
: "${FAKE_MIGRATION_LOG:?FAKE_MIGRATION_LOG is required}"

command_name=""
command_arg=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		-path|-database)
			shift 2
			;;
		version|goto|up)
			command_name="$1"
			shift
			if [ "$#" -gt 0 ]; then
				command_arg="$1"
			fi
			break
			;;
		*) shift ;;
	esac
done

printf 'command=%s arg=%s pgoptions=%s\n' \
	"$command_name" "$command_arg" "${PGOPTIONS:-}" >> "$FAKE_MIGRATION_LOG"

case "$command_name" in
	version)
		state="$(cat "$FAKE_MIGRATION_STATE")"
		case "$state" in
			none)
				echo "no migration"
				exit 1
				;;
			dirty)
				echo "8 (dirty)"
				exit 1
				;;
		esac
		printf '%s\n' "$state"
		;;
	goto)
		printf '%s\n' "$command_arg" > "$FAKE_MIGRATION_STATE"
		;;
	up)
		if [ "$command_arg" != "1" ]; then
			echo "fake migrate only supports up 1" >&2
			exit 1
		fi
		state="$(cat "$FAKE_MIGRATION_STATE")"
		printf '%s\n' "$((state + 1))" > "$FAKE_MIGRATION_STATE"
		;;
	*)
		echo "unsupported fake migrate command: $command_name" >&2
		exit 1
		;;
esac
