// Test-only native plugin: registers an "agy" QuotaProvider and answers per credential (auth_id a.json -> 0.25, else 0.8).
// Plain C on purpose: a second Go runtime in the host process is not needed to prove quota dispatch.
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct { void *ptr; size_t len; } buf_t;
typedef struct { uint32_t abi_version; void *host_ctx; void *call; void *free_buffer; } host_api_t;
typedef struct {
	uint32_t abi_version;
	int (*call)(char *, uint8_t *, size_t, buf_t *);
	void (*free_buffer)(void *, size_t);
	void (*shutdown)(void);
} plugin_api_t;

static int reply(buf_t *out, const char *json) {
	size_t n = strlen(json);
	out->ptr = malloc(n);
	if (!out->ptr) return 1;
	memcpy(out->ptr, json, n);
	out->len = n;
	return 0;
}

static int plugin_call(char *method, uint8_t *req, size_t req_len, buf_t *out) {
	out->ptr = NULL;
	out->len = 0;
	if (!method) return 1;
	if (!strcmp(method, "plugin.register") || !strcmp(method, "plugin.reconfigure"))
		return reply(out, "{\"ok\":true,\"result\":{\"schema_version\":2,\"metadata\":{\"Name\":\"agy-quota-fixture\",\"Version\":\"0.0.0-test\",\"Author\":\"test\",\"GitHubRepository\":\"https://example.invalid/agy-quota-fixture\"},\"capabilities\":{\"quota_provider\":true}}}");
	if (!strcmp(method, "quota.identifier"))
		return reply(out, "{\"ok\":true,\"result\":{\"identifier\":\"agy\"}}");
	if (!strcmp(method, "quota.describe"))
		return reply(out, "{\"ok\":true,\"result\":{\"supported_providers\":[\"agy\"],\"display_name\":\"agy fixture\"}}");
	if (!strcmp(method, "quota.fetch")) {
		char *body = calloc(req_len + 1, 1);
		if (!body) return 1;
		memcpy(body, req, req_len);
		const char *fraction = strstr(body, "\"auth_id\":\"a.json\"") ? "0.25" : "0.8";
		char json[256];
		strcpy(json, "{\"ok\":true,\"result\":{\"groups\":[{\"displayName\":\"Pro\",\"buckets\":[{\"window\":\"5h\",\"remainingFraction\":");
		strcat(json, fraction);
		strcat(json, "}]}]}}");
		free(body);
		return reply(out, json);
	}
	return reply(out, "{\"ok\":true,\"result\":{}}");
}
static void plugin_free(void *p, size_t n) { (void)n; free(p); }
static void plugin_shutdown(void) {}

int cliproxy_plugin_init(host_api_t *host, plugin_api_t *plugin) {
	if (!host || !plugin) return 1;
	plugin->abi_version = 1;
	plugin->call = plugin_call;
	plugin->free_buffer = plugin_free;
	plugin->shutdown = plugin_shutdown;
	return 0;
}
