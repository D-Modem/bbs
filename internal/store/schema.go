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
	prefs      {str} NOT NULL DEFAULT '',
	downloads  {int} NOT NULL DEFAULT 0,
	about      {str} NOT NULL DEFAULT '',
	alias      {str} NOT NULL DEFAULT '',
	signature  {str} NOT NULL DEFAULT '',
	qwk_node   {bool} NOT NULL DEFAULT FALSE
)`, `
CREATE TABLE IF NOT EXISTS areas (
	id          {id},
	name        {name} NOT NULL UNIQUE,
	description {str} NOT NULL DEFAULT '',
	sysop_only  {bool} NOT NULL DEFAULT FALSE,
	newsgroup   {key},
	echotag     {key},
	read_req    {str} NOT NULL DEFAULT '',
	post_req    {str} NOT NULL DEFAULT '',
	grp         {str} NOT NULL DEFAULT '',
	aliases     {bool} NOT NULL DEFAULT FALSE,
	max_msgs    {int} NOT NULL DEFAULT 0,
	max_days    {int} NOT NULL DEFAULT 0
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
	ftn        {blob},
	thread     {int} NOT NULL DEFAULT 0,
	country    {str} NOT NULL DEFAULT ''
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
CREATE TABLE IF NOT EXISTS script_data (
	script  {key} NOT NULL,
	user_id {int} NOT NULL DEFAULT 0,
	name    {key} NOT NULL,
	value   {text} NOT NULL,
	PRIMARY KEY (script, user_id, name)
)`, `
CREATE TABLE IF NOT EXISTS votes (
	message_id {int} NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	user_id    {int} NOT NULL,
	value      {int} NOT NULL,
	PRIMARY KEY (message_id, user_id)
)`, `
CREATE TABLE IF NOT EXISTS poll_answers (
	message_id {int} NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	idx        {int} NOT NULL,
	text       {str} NOT NULL,
	PRIMARY KEY (message_id, idx)
)`, `
CREATE TABLE IF NOT EXISTS poll_votes (
	message_id {int} NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	user_id    {int} NOT NULL,
	idx        {int} NOT NULL,
	PRIMARY KEY (message_id, user_id)
)`, `
CREATE TABLE IF NOT EXISTS chat_rooms (
	name       {key} PRIMARY KEY,
	topic      {str} NOT NULL DEFAULT '',
	created_by {str} NOT NULL DEFAULT '',
	created    {int} NOT NULL
)`, `
CREATE TABLE IF NOT EXISTS qwk_msgs (
	net        {key} NOT NULL,
	number     {int} NOT NULL,
	message_id {int} NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	PRIMARY KEY (net, number)
)`, `
CREATE TABLE IF NOT EXISTS topic_read (
	user_id {int} NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	root    {int} NOT NULL,
	last    {int} NOT NULL,
	PRIMARY KEY (user_id, root)
)`, `
CREATE TABLE IF NOT EXISTS qwk_seen (
	user_id {int} NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	hash    {key} NOT NULL,
	at      {int} NOT NULL,
	PRIMARY KEY (user_id, hash)
)`, `
CREATE TABLE IF NOT EXISTS calls (
	id       {id},
	user_id  {int} NOT NULL,
	name     {str} NOT NULL,
	location {str} NOT NULL DEFAULT '',
	via      {str} NOT NULL DEFAULT '',
	node     {int} NOT NULL DEFAULT 0,
	at       {int} NOT NULL
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
	upload_req   {str} NOT NULL DEFAULT '',
	fileecho     {key}
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
	long_desc   {text},
	origin      {str} NOT NULL DEFAULT '',
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
CREATE TABLE IF NOT EXISTS fido_subs (
	link {key} NOT NULL,
	kind {key} NOT NULL,
	tag  {key} NOT NULL,
	PRIMARY KEY (link, kind, tag)
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
)`, `
CREATE TABLE IF NOT EXISTS chan_boards (
	id            {id},
	dir           {name} NOT NULL UNIQUE,
	title         {str} NOT NULL,
	description   {str} NOT NULL DEFAULT '',
	max_file      {int} NOT NULL DEFAULT 0,
	pages         {int} NOT NULL DEFAULT 10,
	per_page      {int} NOT NULL DEFAULT 10,
	bump_limit    {int} NOT NULL DEFAULT 300,
	image_limit   {int} NOT NULL DEFAULT 150,
	forced_anon   {bool} NOT NULL DEFAULT FALSE,
	default_name  {str} NOT NULL DEFAULT 'Anonymous',
	worksafe      {bool} NOT NULL DEFAULT TRUE,
	locked        {bool} NOT NULL DEFAULT FALSE,
	login_to_post {bool} NOT NULL DEFAULT FALSE,
	text_only     {bool} NOT NULL DEFAULT FALSE,
	mods          {str} NOT NULL DEFAULT '',
	sort          {int} NOT NULL DEFAULT 0,
	last_no       {int} NOT NULL DEFAULT 0,
	created       {int} NOT NULL,
	oekaki        {bool} NOT NULL DEFAULT FALSE,
	login_to_read {bool} NOT NULL DEFAULT FALSE,
	captcha       {str} NOT NULL DEFAULT '',
	options       {str} NOT NULL DEFAULT '',
	poster_ids    {bool} NOT NULL DEFAULT FALSE,
	flags         {bool} NOT NULL DEFAULT FALSE
)`, `
CREATE TABLE IF NOT EXISTS chan_posts (
	id           {id},
	board_id     {int} NOT NULL REFERENCES chan_boards(id) ON DELETE CASCADE,
	num          {int} NOT NULL,
	thread       {int} NOT NULL DEFAULT 0,
	name         {str} NOT NULL DEFAULT '',
	trip         {str} NOT NULL DEFAULT '',
	capcode      {str} NOT NULL DEFAULT '',
	email        {str} NOT NULL DEFAULT '',
	subject      {str} NOT NULL DEFAULT '',
	body         {text} NOT NULL,
	password     {str} NOT NULL DEFAULT '',
	ip           {key} NOT NULL,
	user_id      {int} NOT NULL DEFAULT 0,
	created      {int} NOT NULL,
	bumped       {int} NOT NULL,
	sticky       {bool} NOT NULL DEFAULT FALSE,
	closed       {bool} NOT NULL DEFAULT FALSE,
	file_name    {str} NOT NULL DEFAULT '',
	ext          {str} NOT NULL DEFAULT '',
	tim          {int} NOT NULL DEFAULT 0,
	file_size    {int} NOT NULL DEFAULT 0,
	w            {int} NOT NULL DEFAULT 0,
	h            {int} NOT NULL DEFAULT 0,
	tn_w         {int} NOT NULL DEFAULT 0,
	tn_h         {int} NOT NULL DEFAULT 0,
	md5          {key} NOT NULL DEFAULT '',
	spoiler      {bool} NOT NULL DEFAULT FALSE,
	file_deleted {bool} NOT NULL DEFAULT FALSE,
	banned       {bool} NOT NULL DEFAULT FALSE,
	replay       {bool} NOT NULL DEFAULT FALSE,
	draw_ms      {int} NOT NULL DEFAULT 0,
	account      {str} NOT NULL DEFAULT '',
	poster_id    {key} NOT NULL DEFAULT '',
	country      {str} NOT NULL DEFAULT '',
	country_name {str} NOT NULL DEFAULT ''
)`, `
CREATE TABLE IF NOT EXISTS chan_bans (
	id      {id},
	ip      {key} NOT NULL,
	board   {str} NOT NULL DEFAULT '',
	reason  {str} NOT NULL DEFAULT '',
	by_name {str} NOT NULL DEFAULT '',
	post    {text} NOT NULL,
	created {int} NOT NULL,
	expires {int} NOT NULL DEFAULT 0
)`, `
CREATE TABLE IF NOT EXISTS chan_reports (
	id       {id},
	board_id {int} NOT NULL REFERENCES chan_boards(id) ON DELETE CASCADE,
	num      {int} NOT NULL,
	reason   {str} NOT NULL DEFAULT '',
	ip       {key} NOT NULL,
	created  {int} NOT NULL
)`,
}

// indexes are made if missing.  NULLs do not clash in unique ones, in
// any of the engines.
var indexes = []struct {
	name, table, cols string
	unique            bool
}{
	{"messages_area", "messages", "area_id, id", false},
	{"messages_reply", "messages", "reply_to", false},
	{"mail_to", "mail", "to_id, id", false},
	{"files_uploader", "files", "uploader_id, created", false},
	{"areas_newsgroup", "areas", "newsgroup", true},
	{"messages_message_id", "messages", "message_id", true},
	{"messages_created", "messages", "created", false},
	{"files_created", "files", "created", false},
	{"calls_at", "calls", "at", false},
	{"areas_echotag", "areas", "echotag", true},
	{"messages_ftn_msgid", "messages", "ftn_msgid", false},
	{"api_tokens_user", "api_tokens", "user_id", false},
	{"ssh_keys_user", "ssh_keys", "user_id", false},
	{"qwk_msgs_message", "qwk_msgs", "message_id", false},
	{"messages_thread", "messages", "thread", false},
	{"file_areas_fileecho", "file_areas", "fileecho", true},
	{"chan_posts_num", "chan_posts", "board_id, num", true},
	{"chan_posts_thread", "chan_posts", "board_id, thread, bumped", false},
	{"chan_posts_md5", "chan_posts", "board_id, md5", false},
	{"chan_posts_ip", "chan_posts", "ip, created", false},
	{"chan_reports_post", "chan_reports", "board_id, num", false},
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
	{"files", "long_desc", "{text}"},
	{"users", "downloads", "{int} NOT NULL DEFAULT 0"},
	{"users", "about", "{str} NOT NULL DEFAULT ''"},
	{"users", "alias", "{str} NOT NULL DEFAULT ''"},
	{"users", "signature", "{str} NOT NULL DEFAULT ''"},
	{"areas", "grp", "{str} NOT NULL DEFAULT ''"},
	{"areas", "aliases", "{bool} NOT NULL DEFAULT FALSE"},
	{"areas", "max_msgs", "{int} NOT NULL DEFAULT 0"},
	{"areas", "max_days", "{int} NOT NULL DEFAULT 0"},
	{"users", "qwk_node", "{bool} NOT NULL DEFAULT FALSE"},
	{"messages", "thread", "{int} NOT NULL DEFAULT 0"},
	{"file_areas", "fileecho", "{key}"},
	{"files", "origin", "{str} NOT NULL DEFAULT ''"},
	{"chan_boards", "oekaki", "{bool} NOT NULL DEFAULT FALSE"},
	{"chan_posts", "replay", "{bool} NOT NULL DEFAULT FALSE"},
	{"chan_posts", "draw_ms", "{int} NOT NULL DEFAULT 0"},
	{"chan_posts", "account", "{str} NOT NULL DEFAULT ''"},
	{"chan_boards", "login_to_read", "{bool} NOT NULL DEFAULT FALSE"},
	{"chan_boards", "captcha", "{str} NOT NULL DEFAULT ''"},
	{"chan_boards", "options", "{str} NOT NULL DEFAULT ''"},
	{"chan_boards", "poster_ids", "{bool} NOT NULL DEFAULT FALSE"},
	{"chan_boards", "flags", "{bool} NOT NULL DEFAULT FALSE"},
	{"chan_posts", "poster_id", "{key} NOT NULL DEFAULT ''"},
	{"chan_posts", "country", "{str} NOT NULL DEFAULT ''"},
	{"chan_posts", "country_name", "{str} NOT NULL DEFAULT ''"},
	{"messages", "country", "{str} NOT NULL DEFAULT ''"},
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
	threads := false // messages.thread is new: the threads are to be found
	for _, a := range added {
		ok, err := x.hasColumn(a.table, a.column)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := x.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, a.table, a.column, x.d.ddl(a.decl))); err != nil {
				return err
			}
			threads = threads || a.table == "messages" && a.column == "thread"
		}
	}
	if threads {
		if err := findThreads(x); err != nil {
			return fmt.Errorf("finding the threads: %w", err)
		}
	}
	for _, i := range indexes {
		if err := x.index(i.name, i.table, i.cols, i.unique); err != nil {
			return fmt.Errorf("index %s: %w", i.name, err)
		}
	}
	return nil
}
