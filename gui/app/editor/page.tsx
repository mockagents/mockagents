import { YamlEditor } from "./YamlEditor";
import { validateYAML, ValidateResult } from "@/lib/api";

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

  return <YamlEditor validateAction={validateAction} />;
}
