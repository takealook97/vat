# Schemas

Machine-readable schemas for the formats [`docs/SPEC.md`](../docs/SPEC.md)
specifies. They exist so that a reader which is not a person — an editor, a CI
check, an implementation in another language — can validate a file without
reimplementing the prose.

| File | Format | Draft |
| --- | --- | --- |
| `vat-manifest-v1.schema.json` | workspace manifest, version 1 | JSON Schema 2020-12 |
| `vat-changeset-v1.schema.json` | completion record, version 1 | JSON Schema 2020-12 |
| `vat-brain-record-1.schema.json` | knowledge record front matter, schema 1 | JSON Schema 2020-12 |

## What these are, and are not

`docs/SPEC.md` is the specification. A schema is a projection of it. Where the
two disagree, the document is right and the schema has a bug — a schema cannot
express every rule the format has, and the ones it cannot are not thereby
optional. `workspace.remote_template`, for instance, is checked here for the
`{name}` placeholder but not for an embedded credential, which `vat` refuses.

Each schema is versioned in its filename. A version 2 of a format arrives as a
new file beside the old one; neither the URL nor the contents of a published
schema change meaning after release, because manifests in the wild point at it.

## Validating a file

Every manifest and completion record `vat` writes opens with a modeline, so
editors using `yaml-language-server` validate it with no configuration. Front
matter inside a Markdown record carries none, because no editor looks for one
there; validate those by extracting the front matter first.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/takealook97/vat/main/schemas/vat-manifest-v1.schema.json
```

Anything that speaks JSON Schema 2020-12 works the same way. With
[`check-jsonschema`](https://github.com/python-jsonschema/check-jsonschema):

```bash
check-jsonschema --schemafile schemas/vat-manifest-v1.schema.json vat.yaml
```

## How these are kept honest

A published schema that has drifted from the tool is worse than none: it makes
editors reject files `vat` itself wrote. `internal/cli/schema_contract_test.go`
reads each schema and holds it against the Go types — every property against
the struct's serialised fields in both directions, every enum against the values
`vat` accepts, the version bound against `SchemaVersion`, the name cap against
`ValidateRepoName`, and the modeline URL against the `$id` inside the file it
names. A field added to the manifest and forgotten here fails the suite.

That guard is structural. It does not run a JSON Schema validator, because
`vat` has one runtime dependency and that is a property worth more than the
convenience. Validation against real files is done with an external validator
when a schema changes.
