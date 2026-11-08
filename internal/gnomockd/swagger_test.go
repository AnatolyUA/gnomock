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
	Type                 string                   `yaml:"type"`
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

	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	require.Equal(t, reflect.Struct, typ.Kind(), "%s is not a struct", typ)

	names := make([]string, 0, typ.NumField())

	for f := range typ.Fields() {
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

	doc := loadSwagger(t)

	files, err := filepath.Glob("testdata/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	mismatches := make([]string, 0, len(files))
	for _, file := range files {
		mismatches = append(mismatches, testdataMismatches(t, doc, file)...)
	}

	require.Empty(t, mismatches, "new mismatch between testdata and swagger")
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
			toSet(jsonFieldNames(t, reflect.TypeFor[gnomock.Options]())),
		)...)
		mismatches = append(mismatches, valueTypeMismatches(t, raw, doc.optionsSchema(t),
			fmt.Sprintf("%s: options", base))...)
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
		mismatches = append(mismatches, valueTypeMismatches(t, raw, doc.presetSchema(t, name),
			fmt.Sprintf("%s: preset", base))...)
	}

	return mismatches
}

// valueTypeMismatches reports every key of the raw JSON object whose value
// doesn't match the "type" of the corresponding swagger property. Properties
// without an inline type (e.g. $ref) and unknown keys are skipped; unknown
// keys are reported by unknownKeys.
func valueTypeMismatches(t *testing.T, raw json.RawMessage, schema swaggerSchema, prefix string) []string {
	t.Helper()

	var obj map[string]json.RawMessage

	require.NoError(t, json.Unmarshal(raw, &obj))

	mismatches := []string{}

	for k, v := range obj {
		prop, ok := schema.Properties[k]
		if !ok || prop.Type == "" {
			continue
		}

		if got := jsonValueType(t, v); !swaggerTypeAccepts(prop.Type, got) {
			mismatches = append(mismatches,
				fmt.Sprintf("%s key %q is %s, swagger type is %s", prefix, k, got, prop.Type))
		}
	}

	return mismatches
}

// jsonValueType returns the JSON type of a raw value: string, integer,
// number, boolean, array, object or null.
func jsonValueType(t *testing.T, raw json.RawMessage) string {
	t.Helper()

	var v any

	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	require.NoError(t, dec.Decode(&v))

	switch val := v.(type) {
	case string:
		return "string"
	case json.Number:
		if _, err := val.Int64(); err == nil {
			return "integer"
		}

		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "null"
	}
}

func swaggerTypeAccepts(swaggerType, jsonType string) bool {
	if swaggerType == jsonType {
		return true
	}

	return swaggerType == "number" && jsonType == "integer"
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

	doc := loadSwagger(t)
	mismatches := make([]string, 0, len(swaggerStartPresets))

	for _, name := range swaggerStartPresets {
		p := registry.Find(name)
		require.NotNil(t, p, name)

		goKeys := jsonFieldNames(t, reflect.TypeOf(p))
		swaggerKeys := propertyNames(doc.presetSchema(t, name))

		mismatches = append(mismatches, compareKeys(name, goKeys, swaggerKeys)...)
	}

	require.Empty(t, mismatches, "new mismatch between preset json tags and swagger")
}

func TestSwaggerOptionsJSONTags(t *testing.T) {
	t.Parallel()

	doc := loadSwagger(t)

	goKeys := jsonFieldNames(t, reflect.TypeFor[gnomock.Options]())
	swaggerKeys := propertyNames(doc.optionsSchema(t))

	require.Empty(t, compareKeys("options", goKeys, swaggerKeys),
		"mismatch between gnomock.Options json tags and the swagger options schema")
}

func TestSwaggerResponseJSONTags(t *testing.T) {
	t.Parallel()

	doc := loadSwagger(t)

	container := doc.resolve(t, doc.Components.Schemas["container"])
	mismatches := compareKeys("container",
		jsonFieldNames(t, reflect.TypeFor[gnomock.Container]()), propertyNames(container))

	namedPorts := doc.resolve(t, container.Properties["ports"])
	require.NotNil(t, namedPorts.AdditionalProperties)

	port := doc.resolve(t, *namedPorts.AdditionalProperties)
	mismatches = append(mismatches, compareKeys("named-ports",
		jsonFieldNames(t, reflect.TypeFor[gnomock.Port]()), propertyNames(port))...)

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

	require.Empty(t, mismatches, "new mismatch between response types and swagger")
}

func TestSwaggerValueTypeCheck(t *testing.T) {
	t.Parallel()

	schema := swaggerSchema{Properties: map[string]swaggerSchema{
		"version": {Type: "string"},
		"timeout": {Type: "integer"},
		"ratio":   {Type: "number"},
		"debug":   {Type: "boolean"},
		"env":     {Type: "array"},
		"ref":     {Ref: "#/components/schemas/x"},
	}}

	good := `{"version":"1","timeout":5,"ratio":1,"debug":true,"env":[],"ref":"anything","unknown":1}`
	require.Empty(t, valueTypeMismatches(t, json.RawMessage(good), schema, "x"))

	bad := `{"version":1,"timeout":1.5,"ratio":"1","debug":"true","env":{}}`
	require.ElementsMatch(t, []string{
		`x key "version" is integer, swagger type is string`,
		`x key "timeout" is number, swagger type is integer`,
		`x key "ratio" is string, swagger type is number`,
		`x key "debug" is string, swagger type is boolean`,
		`x key "env" is object, swagger type is array`,
	}, valueTypeMismatches(t, json.RawMessage(bad), schema, "x"))

	// The real swagger options schema must declare inline types, otherwise
	// the testdata type check above would be vacuous.
	opts := loadSwagger(t).optionsSchema(t)
	require.Equal(t, "integer", opts.Properties["timeout"].Type)
}
