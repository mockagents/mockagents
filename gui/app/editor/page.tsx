import { YamlEditor } from "./YamlEditor";
import { validateYAML, saveAgentYAML, ValidateResult, SaveResult } from "@/lib/api";

export default function EditorPage() {
  // The "validate" action runs server-side so we get the auth
  // cookie threaded into the upstream fetch for free. The client
  // component calls this via a useActionState form submission.
  async function validateAction(yaml: string): Promise<ValidateResult> {
    "use server";
    try {
      return await validateYAML(yaml);
    } catch (err) {
      const message = err instanceof Error ? err.message : "unknown error";
      return {
        ok: false,
        kind: "",
        errors: [
          {
            field: "transport",
            message: `Server unreachable: ${message}`,
          },
        ],
      };
    }
  }

  // The "save" action persists the document via the FB-04 write API
  // (create-or-replace), also server-side so the cookie auth is forwarded.
  async function saveAction(yaml: string): Promise<SaveResult> {
    "use server";
    try {
      return await saveAgentYAML(yaml);
    } catch (err) {
      const message = err instanceof Error ? err.message : "unknown error";
      return { ok: false, status: "error", message: `Server unreachable: ${message}` };
    }
  }

  return <YamlEditor validateAction={validateAction} saveAction={saveAction} />;
}
