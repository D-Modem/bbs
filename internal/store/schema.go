package store

import (
	"fmt"
)

// tables is the schema, in the dialect's types: {id} an id that counts
// itself, {int} an integer (unix seconds too), {bool}, {name} a name that
// matches in any case, {key} an indexed string matched exactly, {str} a
// short string, {text} a long one, {blob} bytes.  "read" and "key" are
// quoted: MySQL keeps them for itself.
var tables = []string{`
CREATE TABLE IF NOT EXISTS users (
	id         {id},
	name       {name} NOT NULL UNIQUE,
	password   {str} NOT NULL,
	location   {str} NOT NULL DEFAULT '',
	sysop      {bool} NOT NULL DEFAULT FALSE,
	locked     {bool} NOT NULL DEFAULT FALSE,
	created    {int} NOT NULL,
	last_login {int} NOT NULL DEFAULT 0,
	logins     {int} NOT NULL DEFAULT 0,
	posts      {int} NOT NULL DEFAULT 0,
	level      {int} NOT NULL DEFAULT 20,
	flags      {str} NOT NULL DEFAULT '',
	time_day   {int} NOT NULL DEFAULT 0,
	time_used  {int} NOT NULL DEFAULT 0,
	dl_day     {int} NOT NULL DEFAULT 0,
	dl_count   {int} NOT NULL DEFAULT 0,
	prefs      {str} NOT NULL DEFAULT ''
)`, `
CREATE TABLE IF NOT EXISTS areas (
	id          {id},
	name        {name} NOT NULL UNIQUE,
	description {str} NOT NULL DEFAULT '',
	sysop_only  {bool} NOT NULL DEFAULT FALSE,
	newsgroup   {key},
	echotag     {key},
	read_req    {str} NOT NULL DEFAULT '',
	post_req    {str} NOT NULL DEFAULT ''
)`, `
CREATE TABLE IF NOT EXISTS messages (
	id         {id},
	area_id    {int} NOT NULL REFERENCES areas(id) ON DELETE CASCADE,
	from_id    {int} NOT NULL,
	from_name  {str} NOT NULL,
	to_name    {str} NOT NULL DEFAULT 'All',
	subject    {str} NOT NULL,
	body       {text} NOT NULL,
	reply_to   {int} NOT NULL DEFAULT 0,
	created    {int} NOT NULL,
	message_id {key},
	origin     {str} NOT NULL DEFAULT '',
	raw        {blob},
	ftn_msgid  {key},
	ftn        {blob}
)`, `
CREATE TABLE IF NOT EXISTS lastread (
	user_id    {int} NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	area_id    {int} NOT NULL REFERENCES areas(id) ON DELETE CASCADE,
	message_id {int} NOT NULL,
	PRIMARY KEY (user_id, area_id)
)`, `
CREATE TABLE IF NOT EXISTS scan_off (
	user_id {int} NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	area_id {int} NOT NULL REFERENCES areas(id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, area_id)
)`, `
CREATE TABLE IF NOT EXISTS mail (
	id        {id},
	from_id   {int} NOT NULL,
	from_name {str} NOT NULL,
	to_id     {int} NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	to_name   {str} NOT NULL,
	subject   {str} NOT NULL,
	body      {text} NOT NULL,
	reply_to  {int} NOT NULL DEFAULT 0,
	created   {int} NOT NULL,
	"read"    {bool} NOT NULL DEFAULT FALSE,
	ftn_msgid {key}
)`, `
CREATE TABLE IF NOT EXISTS file_areas (
	id           {id},
	name         {name} NOT NULL UNIQUE,
	description  {str} NOT NULL DEFAULT '',
	sysop_upload {bool} NOT NULL DEFAULT FALSE,
	download_req {str} NOT NULL DEFAULT '',
	upload_req   {str} NOT NULL DEFAULT ''
)`, `
CREATE TABLE IF NOT EXISTS files (
	id          {id},
	area_id     {int} NOT NULL REFERENCES file_areas(id),
	name        {name} NOT NULL,
	description {str} NOT NULL DEFAULT '',
	size        {int} NOT NULL,
	sha256      {key} NOT NULL,
	uploader_id {int} NOT NULL,
	uploader    {str} NOT NULL,
	created     {int} NOT NULL,
	downloads   {int} NOT NULL DEFAULT 0,
	UNIQUE (area_id, name)
)`, `
CREATE TABLE IF NOT EXISTS history (
	message_id {key} PRIMARY KEY,
	seen       {int} NOT NULL
)`, `
CREATE TABLE IF NOT EXISTS peer_groups (
	peer      {key} NOT NULL,
	newsgroup {key} NOT NULL,
	last      {int} NOT NULL,
	PRIMARY KEY (peer, newsgroup)
)`, `
CREATE TABLE IF NOT EXISTS peer_push (
	peer    {key} PRIMARY KEY,
	last_id {int} NOT NULL
)`, `
CREATE TABLE IF NOT EXISTS netmail_out (
	id        {id},
	from_id   {int} NOT NULL,
	from_name {str} NOT NULL,
	to_name   {str} NOT NULL,
	to_addr   {str} NOT NULL,
	subject   {str} NOT NULL,
	body      {text} NOT NULL,
	msgid     {str} NOT NULL,
	reply     {str} NOT NULL DEFAULT '',
	created   {int} NOT NULL,
	packed    {int} NOT NULL DEFAULT 0
)`, `
CREATE TABLE IF NOT EXISTS api_tokens (
	id        {id},
	user_id   {int} NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name      {str} NOT NULL,
	hash      {key} NOT NULL UNIQUE,
	scopes    {str} NOT NULL,
	session   {bool} NOT NULL DEFAULT FALSE,
	created   {int} NOT NULL,
	expires   {int} NOT NULL DEFAULT 0,
	last_used {int} NOT NULL DEFAULT 0
)`, `
CREATE TABLE IF NOT EXISTS ssh_keys (
	id          {id},
	user_id     {int} NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	type        {str} NOT NULL,
	fingerprint {key} NOT NULL UNIQUE,
	"key"       {text} NOT NULL,
	comment     {str} NOT NULL DEFAULT '',
	created     {int} NOT NULL,
	last_used   {int} NOT NULL DEFAULT 0
)`,
}

// indexes are made if missing.  NULLs do not clash in unique ones, in
// any of the engines.
var indexes = []struct {
	name, table, cols string
	unique            bool
}{
	{"messages_area", "messages", "area_id, id", false},
	{"mail_to", "mail", "to_id, id", false},
	{"files_uploader", "files", "uploader_id, created", false},
	{"areas_newsgroup", "areas", "newsgroup", true},
	{"messages_message_id", "messages", "message_id", true},
	{"messages_created", "messages", "created", false},
	{"areas_echotag", "areas", "echotag", true},
	{"messages_ftn_msgid", "messages", "ftn_msgid", false},
	{"api_tokens_user", "api_tokens", "user_id", false},
	{"ssh_keys_user", "ssh_keys", "user_id", false},
}

// added are the columns added since the first schema: an older database
// (SQLite, from before) gets them.
var added = []struct{ table, column, decl string }{
	{"areas", "newsgroup", "{key}"},
	{"messages", "message_id", "{key}"},
	{"messages", "origin", "{str} NOT NULL DEFAULT ''"},
	{"messages", "raw", "{blob}"},
	{"areas", "echotag", "{key}"},
	{"messages", "ftn_msgid", "{key}"},
	{"messages", "ftn", "{blob}"},
	{"mail", "ftn_msgid", "{key}"},
	{"users", "level", "{int} NOT NULL DEFAULT 20"},
	{"users", "flags", "{str} NOT NULL DEFAULT ''"},
	{"users", "time_day", "{int} NOT NULL DEFAULT 0"},
	{"users", "time_used", "{int} NOT NULL DEFAULT 0"},
	{"users", "dl_day", "{int} NOT NULL DEFAULT 0"},
	{"users", "dl_count", "{int} NOT NULL DEFAULT 0"},
	{"areas", "read_req", "{str} NOT NULL DEFAULT ''"},
	{"areas", "post_req", "{str} NOT NULL DEFAULT ''"},
	{"file_areas", "download_req", "{str} NOT NULL DEFAULT ''"},
	{"file_areas", "upload_req", "{str} NOT NULL DEFAULT ''"},
	{"users", "prefs", "{str} NOT NULL DEFAULT ''"},
}

// migrate makes the schema, or brings an older database up to it.
func migrate(x *db) error {
	if x.d.name == Postgres {
		// names that match in any case: ICU's, at strength 2
		if _, err := x.Exec(`CREATE COLLATION IF NOT EXISTS nocase (provider = icu, locale = 'und-u-ks-level2', deterministic = false)`); err != nil {
			return fmt.Errorf("the nocase collation: %w", err)
		}
	}
	for _, t := range tables {
		if _, err := x.Exec(x.d.ddl(t)); err != nil {
			return err
		}
	}
	for _, a := range added {
		ok, err := x.hasColumn(a.table, a.column)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := x.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, a.table, a.column, x.d.ddl(a.decl))); err != nil {
				return err
			}
		}
	}
	for _, i := range indexes {
		if err := x.index(i.name, i.table, i.cols, i.unique); err != nil {
			return fmt.Errorf("index %s: %w", i.name, err)
		}
	}
	return nil
}
