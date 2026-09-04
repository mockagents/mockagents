"use client";

// UX-06: the export control, including the included-data review.
//
// Two rules shape this component:
//
//   1. Raw bodies are opt-in AND reviewed. Ticking the box does not arm the
//      export; it opens a review that says how many payloads would be embedded
//      and that nothing is redacted. Only an explicit acknowledgement of that
//      review arms the with-bodies export. One click must not be able to put a
//      captured credential in a file the operator then emails.
//   2. The file is built here from the same pure module the page renders from,
//      so what is downloaded is what was described on screen — there is no
//      second, differently-hedged serialization path.
//
// The rows arrive from the server component already loaded (the logs API
// returns bodies inline; there is no metadata-only read), so opting in changes
// what is WRITTEN TO THE FILE, not what the console fetched.

import { useState } from "react";

import {
  buildReport,
  reportToJSON,
  reportToPrintableHTML,
  reviewIncludedData,
  type BuildReportInput,
} from "@/lib/report";
import { Icon } from "@/lib/icons";

export interface ReportExportProps {
  /** Everything buildReport needs except the bodies decision. */
  input: Omit<BuildReportInput, "includeBodies">;
}

function download(filename: string, mime: string, contents: string) {
  const blob = new Blob([contents], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.rel = "noopener";
  document.body.appendChild(a);
  a.click();
  a.remove();
  // Revoke on the next tick: revoking synchronously can cancel the download in
  // some browsers before it has read the blob.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

function stamp(): string {
  return new Date().toISOString().replace(/[:.]/g, "-");
}

export function ReportExport({ input }: ReportExportProps) {
  const [wantBodies, setWantBodies] = useState(false);
  const [reviewed, setReviewed] = useState(false);

  // Bodies ride along only when the reviewer has BOTH asked for them and
  // acknowledged what they contain.
  const includeBodies = wantBodies && reviewed;
  const review = reviewIncludedData(input.rows);

  function make() {
    return buildReport({ ...input, includeBodies });
  }

  function downloadJSON() {
    download(`mockagents-evidence-${stamp()}.json`, "application/json", reportToJSON(make()));
  }

  function downloadHTML() {
    download(`mockagents-evidence-${stamp()}.html`, "text/html", reportToPrintableHTML(make()));
  }

  return (
    <div className="col gap-3">
      <label className="receipt-line" style={{ cursor: "pointer" }}>
        <input
          type="checkbox"
          checked={wantBodies}
          onChange={(e) => {
            setWantBodies(e.target.checked);
            // Un-ticking clears the acknowledgement, so re-ticking has to be
            // reviewed again rather than silently reusing an old consent.
            if (!e.target.checked) setReviewed(false);
          }}
        />
        <span className="txt-sm">
          Include raw request and response bodies
          <span className="muted"> — requires review</span>
        </span>
      </label>

      {wantBodies && (
        <div className="banner banner-warn" role="region" aria-label="included data review">
          <div className="col gap-2 grow">
            <div>
              <strong>Review what would leave this browser.</strong>
            </div>
            <dl className="kv">
              <dt>request bodies</dt>
              <dd className="mono">{review.requestBodies}</dd>
              <dt>response bodies</dt>
              <dd className="mono">{review.responseBodies}</dd>
              <dt>payload size</dt>
              <dd className="mono">{review.characters.toLocaleString()} characters</dd>
              <dt>clipped captures</dt>
              <dd className="mono">
                {review.truncatedCaptures}
                {review.truncatedCaptures > 0 ? " — exported as fragments" : ""}
              </dd>
            </dl>
            <p className="txt-sm" style={{ margin: 0 }}>
              These payloads are embedded <strong>verbatim</strong>. This console applies
              no redaction, so any credential, token or personal data captured in a body
              will be in the file.
            </p>
            <label className="row gap-2" style={{ cursor: "pointer" }}>
              <input
                type="checkbox"
                checked={reviewed}
                onChange={(e) => setReviewed(e.target.checked)}
              />
              <span className="txt-sm">
                I have reviewed the included data and accept that it is unredacted.
              </span>
            </label>
          </div>
        </div>
      )}

      <div className="row gap-2 wrap">
        <button type="button" className="btn btn-default btn-sm" onClick={downloadJSON}>
          <Icon name="save" size={15} />
          Download JSON
        </button>
        <button type="button" className="btn btn-outline btn-sm" onClick={downloadHTML}>
          <Icon name="file-code" size={15} />
          Printable HTML
        </button>
        <span className="muted txt-xs">
          {includeBodies
            ? "Bodies WILL be embedded, unredacted."
            : wantBodies
              ? "Bodies are not armed yet — acknowledge the review above."
              : "Metadata only. Bodies are excluded."}
        </span>
      </div>
    </div>
  );
}
