import { readFile } from "node:fs/promises";
import path from "node:path";

import yaml from "js-yaml";

/*
 * The API reference is read from api/openapi.yaml at build time.
 *
 * That is the whole point: the same file generates the server's handler
 * signatures and the client's types, so documentation built from it cannot
 * describe an endpoint the implementation does not have.
 */

export interface Operation {
  method: string;
  path: string;
  summary: string;
  description: string;
  tag: string;
  parameters: { name: string; in: string; required: boolean; description: string }[];
}

interface RawParameter {
  name?: string;
  in?: string;
  required?: boolean;
  description?: string;
  $ref?: string;
}

interface RawOperation {
  operationId?: string;
  summary?: string;
  description?: string;
  tags?: string[];
  parameters?: RawParameter[];
}

interface RawSpec {
  info?: { title?: string; version?: string; description?: string };
  paths?: Record<string, Record<string, RawOperation | RawParameter[]>>;
  components?: { parameters?: Record<string, RawParameter> };
}

const METHODS = ["get", "post", "put", "patch", "delete"] as const;

export interface ApiReference {
  title: string;
  version: string;
  description: string;
  operations: Operation[];
}

export async function loadApiReference(): Promise<ApiReference> {
  // Two levels up from site/: the specification is shared, not copied.
  const file = path.join(process.cwd(), "..", "api", "openapi.yaml");
  const spec = yaml.load(await readFile(file, "utf8")) as RawSpec;

  const shared = spec.components?.parameters ?? {};
  const operations: Operation[] = [];

  for (const [route, methods] of Object.entries(spec.paths ?? {})) {
    const pathLevel = (methods.parameters as RawParameter[] | undefined) ?? [];

    for (const method of METHODS) {
      const operation = methods[method] as RawOperation | undefined;
      if (!operation) continue;

      const parameters = [...pathLevel, ...(operation.parameters ?? [])]
        .map((parameter) => resolve(parameter, shared))
        .filter((parameter): parameter is Required<Pick<RawParameter, "name" | "in">> & RawParameter =>
          Boolean(parameter.name && parameter.in),
        )
        .map((parameter) => ({
          name: parameter.name,
          in: parameter.in,
          required: parameter.required ?? parameter.in === "path",
          description: parameter.description ?? "",
        }));

      operations.push({
        method: method.toUpperCase(),
        path: route,
        summary: operation.summary ?? "",
        description: operation.description ?? "",
        tag: operation.tags?.[0] ?? "other",
        parameters,
      });
    }
  }

  return {
    title: spec.info?.title ?? "Phenk",
    version: spec.info?.version ?? "0",
    description: spec.info?.description ?? "",
    operations,
  };
}

/** Resolves a local $ref into the shared parameter it names. */
function resolve(parameter: RawParameter, shared: Record<string, RawParameter>): RawParameter {
  if (!parameter.$ref) return parameter;
  const name = parameter.$ref.split("/").pop();
  return (name && shared[name]) || parameter;
}
