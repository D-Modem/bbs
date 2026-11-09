# Runs one of acme.sh's dnsapi scripts with acme.sh's functions loaded:
#   sh -c "$(cat acmesh.sh)" acmesh ACME.SH dns_xx add|rm FQDN VALUE
# acme.sh's last line runs its arguments as a command, so it is given
# "true" to load quietly; LE_WORKING_DIR is its home (account.conf).
acmesh=$1 api=$2 action=$3 fqdn=$4 value=$5
_SCRIPT_HOME=$(dirname "$acmesh")
: "${LE_WORKING_DIR:=$_SCRIPT_HOME}"
export LE_WORKING_DIR
set -- true
. "$acmesh" || exit 1
_initpath
d_api=$(_findHook "" dnsapi "$api")
[ -n "$d_api" ] || { echo "acme.sh has no dnsapi/$api.sh" >&2; exit 1; }
. "$d_api" || exit 1
"${api}_$action" "$fqdn" "$value"
