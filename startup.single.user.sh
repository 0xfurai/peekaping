# Sourced as root by the bundle startup scripts, before they start Peekaping.
#
# Peekaping runs as the peekaping user: peekaping in the SQLite and PostgreSQL
# bundles (su-exec or gosu). Each switch gives it only the groups
# /etc/group gives the user: --group-add and cap_add don't reach it. So the
# user's IDs and groups are settled here.

require_number() {
    case "$2" in
        '' | *[!0-9]*)
            echo "ERROR: $1 must be a number, got '$2'"
            exit 1
            ;;
    esac
}

PUID=${PUID:-1000}
PGID=${PGID:-1000}
require_number PUID "$PUID"
require_number PGID "$PGID"

if [ "$(id -g peekaping)" != "$PGID" ]; then
    taken=$(getent group "$PGID" | cut -d: -f1)
    if [ -n "$taken" ]; then
        echo "ERROR: PGID $PGID is already used by the '$taken' group in the image; choose another PGID"
        exit 1
    fi
    groupmod -g "$PGID" peekaping
fi

if [ "$(id -u peekaping)" != "$PUID" ]; then
    taken=$(getent passwd "$PUID" | cut -d: -f1)
    if [ -n "$taken" ]; then
        echo "ERROR: PUID $PUID is already used by the '$taken' user in the image; choose another PUID"
        exit 1
    fi
    usermod -u "$PUID" peekaping
fi

# Docker monitors use a socket mounted at the default path, so give the user its group.
docker_socket=/var/run/docker.sock
if [ -S "$docker_socket" ]; then
    socket_gid=$(stat -c %g "$docker_socket")
    socket_group=$(getent group "$socket_gid" | cut -d: -f1)
    if [ -z "$socket_group" ]; then
        socket_group="docker-$socket_gid"
        groupadd -g "$socket_gid" "$socket_group"
    fi
    usermod -aG "$socket_group" peekaping

    socket_mode=$(stat -c %a "$docker_socket")
    if [ $(((0$socket_mode >> 3) & 6)) -ne 6 ]; then
        echo "WARNING: Docker monitors can't use $docker_socket: its group can't read and write it (mode $socket_mode)."
    fi
fi

# Ping monitors open unprivileged ICMP sockets, which Linux allows only for
# groups in net.ipv4.ping_group_range.
ping_range=/proc/sys/net/ipv4/ping_group_range
if [ -r "$ping_range" ]; then
    # Not `read`: the kernel ends the file after the first byte of a byte-by-byte read.
    ping_low=$(cut -f1 "$ping_range")
    ping_high=$(cut -f2 "$ping_range")
    ping_allowed=false
    for gid in $(id -G peekaping); do
        if [ "$gid" -ge "$ping_low" ] && [ "$gid" -le "$ping_high" ]; then
            ping_allowed=true
        fi
    done
    if [ "$ping_allowed" = false ]; then
        echo "WARNING: ping monitors will fail: net.ipv4.ping_group_range ($ping_low $ping_high) doesn't include PGID $PGID."
        echo "WARNING: Allow it with: docker run --sysctl net.ipv4.ping_group_range=\"0 2147483647\" ..."
    fi
fi

mkdir -p /app/data
# Some volumes (NFS with root_squash, CIFS) don't let root change owners. The
# files may already belong to PUID/PGID there, so carry on.
if ! chown -R peekaping:peekaping /app/data; then
    echo "WARNING: couldn't give /app/data to $PUID:$PGID. If the database can't be written, give the folder to $PUID:$PGID on the host, or set PUID/PGID to its owner."
fi
