package gnomockd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/errors"
	"github.com/orlangure/gnomock/internal/registry"
	_ "github.com/orlangure/gnomock/preset/azurite"
	_ "github.com/orlangure/gnomock/preset/cassandra"
	_ "github.com/orlangure/gnomock/preset/cockroachdb"
	_ "github.com/orlangure/gnomock/preset/elastic"
	_ "github.com/orlangure/gnomock/preset/influxdb"
	_ "github.com/orlangure/gnomock/preset/k3s"
	_ "github.com/orlangure/gnomock/preset/kafka"
	_ "github.com/orlangure/gnomock/preset/localstack"
	_ "github.com/orlangure/gnomock/preset/mariadb"
	_ "github.com/orlangure/gnomock/preset/memcached"
	_ "github.com/orlangure/gnomock/preset/mongo"
	_ "github.com/orlangure/gnomock/preset/mssql"
	_ "github.com/orlangure/gnomock/preset/mysql"
	_ "github.com/orlangure/gnomock/preset/postgres"
	_ "github.com/orlangure/gnomock/preset/rabbitmq"
	_ "github.com/orlangure/gnomock/preset/redis"
	_ "github.com/orlangure/gnomock/preset/splunk"
	_ "github.com/orlangure/gnomock/preset/vault"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const swaggerPath = "../../swagger/swagger.yaml"

// swaggerStartPresets are the preset names served under /start/{name}, as
// registered in internal/registry and documented in swagger.yaml.
var swaggerStartPresets = []string{
	"azurite", "cassandra", "cockroachdb", "elastic", "influxdb", "kafka",
	"kubernetes", "localstack", "mariadb", "memcached", "mongo", "mssql",
	"mysql", "postgres", "rabbitmq", "redis", "splunk", "vault",
}

// testdataPresetAliases maps testdata file name prefixes to preset names
// when they differ.
var testdataPresetAliases = map[string]string{
	"k3s": "kubernetes",
}

type swaggerSchema struct {
	Ref                  string                   `yaml:"$ref"`
	Properties           map[string]swaggerSchema `yaml:"properties"`
	AdditionalProperties *swaggerSchema           `yaml:"additionalProperties"`
}

type swaggerDoc struct {
	Paths map[string]struct {
		Post struct {
			RequestBody struct {
				Content map[string]struct {
					Schema swaggerSchema `yaml:"schema"`
				} `yaml:"content"`
			} `yaml:"requestBody"`
		} `yaml:"post"`
	} `yaml:"paths"`
	Components struct {
		Schemas map[string]swaggerSchema `yaml:"schemas"`
	} `yaml:"components"`
}

func loadSwagger(t *testing.T) *swaggerDoc {
	t.Helper()

	bs, err := os.ReadFile(swaggerPath)
	require.NoError(t, err)

	doc := &swaggerDoc{}
	require.NoError(t, yaml.Unmarshal(bs, doc))
	require.NotEmpty(t, doc.Paths)
	require.NotEmpty(t, doc.Components.Schemas)

	return doc
}

// resolve follows a local "#/components/schemas/<name>" reference.
func (d *swaggerDoc) resolve(t *testing.T, s swaggerSchema) swaggerSchema {
	t.Helper()

	if s.Ref == "" {
		return s
	}

	const prefix = "#/components/schemas/"

	require.True(t, strings.HasPrefix(s.Ref, prefix), "unexpected ref %q", s.Ref)

	resolved, ok := d.Components.Schemas[strings.TrimPrefix(s.Ref, prefix)]
	require.True(t, ok, "unresolved ref %q", s.Ref)

	return d.resolve(t, resolved)
}

// requestSchema returns the request body schema of /start/{name}.
func (d *swaggerDoc) requestSchema(t *testing.T, name string) swaggerSchema {
	t.Helper()

	path, ok := d.Paths["/start/"+name]
	require.True(t, ok, "swagger has no /start/%s path", name)

	content, ok := path.Post.RequestBody.Content["application/json"]
	require.True(t, ok, "/start/%s has no application/json request body", name)

	return d.resolve(t, content.Schema)
}

// presetSchema returns the schema of the "preset" property of /start/{name}.
func (d *swaggerDoc) presetSchema(t *testing.T, name string) swaggerSchema {
	t.Helper()

	req := d.requestSchema(t, name)

	preset, ok := req.Properties["preset"]
	require.True(t, ok, "/start/%s request has no preset property", name)

	return d.resolve(t, preset)
}

func (d *swaggerDoc) optionsSchema(t *testing.T) swaggerSchema {
	t.Helper()

	options, ok := d.Components.Schemas["options"]
	require.True(t, ok)

	return d.resolve(t, options)
}

// jsonFieldNames returns the JSON object keys encoding/json uses for the
// exported fields of struct type typ (pointers are dereferenced). Fields
// tagged "-" are skipped, untagged exported fields use the Go field name.
func jsonFieldNames(t *testing.T, typ reflect.Type) []string {
	t.Helper()

	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	require.Equal(t, reflect.Struct, typ.Kind(), "%s is not a struct", typ)

	names := make([]string, 0, typ.NumField())

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}

		// Embedded structs would need flattening; none exist today.
		require.False(t, f.Anonymous, "embedded field %s.%s is not supported by this test", typ, f.Name)

		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}

		if name == "" {
			name = f.Name
		}

		names = append(names, name)
	}

	return names
}

func propertyNames(s swaggerSchema) []string {
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}

	return set
}

// compareKeys returns one mismatch line for every Go JSON key missing from
// swagger and for every swagger property missing from Go.
func compareKeys(scope string, goKeys, swaggerKeys []string) []string {
	goSet, swaggerSet := toSet(goKeys), toSet(swaggerKeys)
	mismatches := []string{}

	for _, k := range goKeys {
		if !swaggerSet[k] {
			mismatches = append(mismatches, fmt.Sprintf("%s: go field %q missing from swagger", scope, k))
		}
	}

	for _, k := range swaggerKeys {
		if !goSet[k] {
			mismatches = append(mismatches, fmt.Sprintf("%s: swagger property %q missing from go", scope, k))
		}
	}

	return mismatches
}

func TestSwaggerStartPathsMatchRegistry(t *testing.T) {
	t.Parallel()

	doc := loadSwagger(t)

	paths := []string{}

	for p := range doc.Paths {
		if name, ok := strings.CutPrefix(p, "/start/"); ok {
			paths = append(paths, name)
		}
	}

	require.ElementsMatch(t, swaggerStartPresets, paths)

	for _, name := range swaggerStartPresets {
		require.NotNil(t, registry.Find(name), "preset %q is not registered", name)
	}
}

func TestSwaggerTestdataRequestKeys(t *testing.T) {
	t.Parallel()

	// Known gap: vault.json nests "options" inside "preset" instead of
	// putting it at the top level. Encoding/json silently ignores the unknown
	// key today. Remove this entry once the testdata file is fixed.
	knownGaps := []string{
		`vault.json: preset key "options" is not a swagger vault property`,
		`vault.json: preset key "options" is not a go field of the vault preset`,
		// Known gap: the cockroachdb swagger schema documents only "version"
		// (see TestSwaggerPresetJSONTags). Fix swagger.yaml in PLAN.md Phase 7.
		`cockroachdb.json: preset key "db" is not a swagger cockroachdb property`,
		`cockroachdb.json: preset key "queries" is not a swagger cockroachdb property`,
		`cockroachdb.json: preset key "queries_files" is not a swagger cockroachdb property`,
	}

	doc := loadSwagger(t)

	files, err := filepath.Glob("testdata/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	mismatches := []string{}
	for _, file := range files {
		mismatches = append(mismatches, testdataMismatches(t, doc, file)...)
	}

	require.ElementsMatch(t, knownGaps, mismatches,
		"new mismatch between testdata and swagger, or a known gap was fixed: update knownGaps")
}

// testdataMismatches checks the keys of one gnomockd request body against
// swagger and against the Go types the body is decoded into.
func testdataMismatches(t *testing.T, doc *swaggerDoc, file string) []string {
	t.Helper()

	base := filepath.Base(file)
	name, _, _ := strings.Cut(strings.TrimSuffix(base, ".json"), "_")

	if alias, ok := testdataPresetAliases[name]; ok {
		name = alias
	}

	require.Contains(t, swaggerStartPresets, name, "%s does not map to a known preset", base)

	bs, err := os.ReadFile(file)
	require.NoError(t, err)

	var body map[string]json.RawMessage

	require.NoError(t, json.Unmarshal(bs, &body), base)

	mismatches := []string{}
	requestKeys := toSet(propertyNames(doc.requestSchema(t, name)))

	for k := range body {
		if !requestKeys[k] {
			mismatches = append(mismatches, fmt.Sprintf("%s: top-level key %q is not a swagger request property", base, k))
		}
	}

	if raw, ok := body["options"]; ok {
		mismatches = append(mismatches, unknownKeys(t, raw,
			fmt.Sprintf("%s: options key %%q is not a swagger options property", base),
			toSet(propertyNames(doc.optionsSchema(t))),
			fmt.Sprintf("%s: options key %%q is not a go field of gnomock.Options", base),
			toSet(jsonFieldNames(t, reflect.TypeOf(gnomock.Options{}))),
		)...)
	}

	if raw, ok := body["preset"]; ok {
		p := registry.Find(name)
		require.NotNil(t, p, name)

		mismatches = append(mismatches, unknownKeys(t, raw,
			fmt.Sprintf("%s: preset key %%q is not a swagger %s property", base, name),
			toSet(propertyNames(doc.presetSchema(t, name))),
			fmt.Sprintf("%s: preset key %%q is not a go field of the %s preset", base, name),
			toSet(jsonFieldNames(t, reflect.TypeOf(p))),
		)...)
	}

	return mismatches
}

// unknownKeys reports every key of the raw JSON object that is missing from
// swaggerKeys or goKeys, formatted with the matching message format.
func unknownKeys(
	t *testing.T, raw json.RawMessage,
	swaggerMsg string, swaggerKeys map[string]bool,
	goMsg string, goKeys map[string]bool,
) []string {
	t.Helper()

	var obj map[string]json.RawMessage

	require.NoError(t, json.Unmarshal(raw, &obj))

	mismatches := []string{}

	for k := range obj {
		if !swaggerKeys[k] {
			mismatches = append(mismatches, fmt.Sprintf(swaggerMsg, k))
		}

		if !goKeys[k] {
			mismatches = append(mismatches, fmt.Sprintf(goMsg, k))
		}
	}

	return mismatches
}

func TestSwaggerPresetJSONTags(t *testing.T) {
	t.Parallel()

	knownGaps := []string{
		// Known gap: k3s exposes untagged exported fields that are decoded
		// from JSON by their Go names but are not documented. Not scheduled
		// in PLAN.md yet.
		`kubernetes: go field "Port" missing from swagger`,
		`kubernetes: go field "UseDynamicPort" missing from swagger`,
		`kubernetes: go field "K3sServerFlags" missing from swagger`,
		// Known gap: cockroachdb data loading fields are not documented.
		// Fix swagger.yaml in PLAN.md Phase 7.
		`cockroachdb: go field "db" missing from swagger`,
		`cockroachdb: go field "queries" missing from swagger`,
		`cockroachdb: go field "queries_files" missing from swagger`,
		// Known gap: kafka topic_configs is not documented. Fix swagger.yaml
		// in PLAN.md Phase 7.
		`kafka: go field "topic_configs" missing from swagger`,
	}

	doc := loadSwagger(t)
	mismatches := []string{}

	for _, name := range swaggerStartPresets {
		p := registry.Find(name)
		require.NotNil(t, p, name)

		goKeys := jsonFieldNames(t, reflect.TypeOf(p))
		swaggerKeys := propertyNames(doc.presetSchema(t, name))

		mismatches = append(mismatches, compareKeys(name, goKeys, swaggerKeys)...)
	}

	require.ElementsMatch(t, knownGaps, mismatches,
		"new mismatch between preset json tags and swagger, or a known gap was fixed: update knownGaps")
}

func TestSwaggerOptionsJSONTags(t *testing.T) {
	t.Parallel()

	// Known gap, fixed in PLAN.md Phase 4/7.
	knownGaps := []string{
		`options: go field "entrypoint" missing from swagger`,
		`options: go field "host_mounts" missing from swagger`,
		`options: go field "extraHosts" missing from swagger`,
		`options: go field "reuse" missing from swagger`,
		`options: go field "customImage" missing from swagger`,
		`options: go field "user" missing from swagger`,
	}

	doc := loadSwagger(t)

	goKeys := jsonFieldNames(t, reflect.TypeOf(gnomock.Options{}))
	swaggerKeys := propertyNames(doc.optionsSchema(t))

	require.ElementsMatch(t, knownGaps, compareKeys("options", goKeys, swaggerKeys),
		"new mismatch between gnomock.Options json tags and swagger, or a known gap was fixed: update knownGaps")
}

func TestSwaggerResponseJSONTags(t *testing.T) {
	t.Parallel()

	knownGaps := []string{
		// Known gap: the response port objects include "host_port" (always 0
		// in responses), but swagger named-ports doesn't document it. Not
		// scheduled in PLAN.md yet.
		`named-ports: go field "host_port" missing from swagger`,
		// Known gap: start/stop failures may include the "container" object,
		// but swagger documents only "error". Not scheduled in PLAN.md yet.
		`start-failed: go field "container" missing from swagger`,
		`stop-failed: go field "container" missing from swagger`,
	}

	doc := loadSwagger(t)

	container := doc.resolve(t, doc.Components.Schemas["container"])
	mismatches := compareKeys("container",
		jsonFieldNames(t, reflect.TypeOf(gnomock.Container{})), propertyNames(container))

	namedPorts := doc.resolve(t, container.Properties["ports"])
	require.NotNil(t, namedPorts.AdditionalProperties)

	port := doc.resolve(t, *namedPorts.AdditionalProperties)
	mismatches = append(mismatches, compareKeys("named-ports",
		jsonFieldNames(t, reflect.TypeOf(gnomock.Port{})), propertyNames(port))...)

	// Error types are unexported, so their JSON keys are taken from the
	// encoded errors, with every optional field populated.
	rootErr := fmt.Errorf("boom")
	errorSchemas := map[string]error{
		"invalid-start-request": errors.NewInvalidStartRequestError(rootErr),
		"start-failed":          errors.NewStartFailedError(rootErr, &gnomock.Container{}),
		"stop-failed":           errors.StopFailedError(rootErr, &gnomock.Container{}),
		"invalid-stop-request":  errors.InvalidStopRequestError(rootErr),
	}

	for schema, e := range errorSchemas {
		s, ok := doc.Components.Schemas[schema]
		require.True(t, ok, schema)

		bs, err := json.Marshal(e)
		require.NoError(t, err)

		var encoded map[string]json.RawMessage

		require.NoError(t, json.Unmarshal(bs, &encoded))

		goKeys := make([]string, 0, len(encoded))
		for k := range encoded {
			goKeys = append(goKeys, k)
		}

		sort.Strings(goKeys)

		mismatches = append(mismatches, compareKeys(schema, goKeys, propertyNames(doc.resolve(t, s)))...)
	}

	require.ElementsMatch(t, knownGaps, mismatches,
		"new mismatch between response types and swagger, or a known gap was fixed: update knownGaps")
}
