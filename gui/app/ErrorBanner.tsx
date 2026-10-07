// The banner for a `?error=` outcome (review K-18).
//
// `code` comes from the URL, so it is treated as untrusted: it selects one of
// the fixed messages in lib/errors.ts, and anything unrecognised renders the
// generic message. The value itself is never shown. `detail` is the upstream's
// own message delivered through the server-side flash store — not something a
// link can set — and is only shown alongside a code.

import { errorMessage } from "@/lib/errors";
import { Icon } from "@/lib/icons";

export interface ErrorBannerProps {
  /** The raw `?error=` value. */
  code: string | string[] | undefined;
  /** Bold lead-in, e.g. "Login failed." */
  title?: string;
  /** Flashed upstream detail, already bounded. */
  detail?: string | null;
  /** Render with the leading x-circle icon used on the admin pages. */
  icon?: boolean;
}

export function ErrorBanner({ code, title, detail, icon = false }: ErrorBannerProps) {
  const message = errorMessage(code);
  if (!message) return null;
  const body = (
    <div>
      {title && <strong>{title}</strong>} {message}
      {detail && <span className="error-detail"> Server said: {detail}</span>}
    </div>
  );
  return (
    <div className="banner banner-error" role="alert">
      {icon ? (
        <div className="row gap-2">
          <Icon name="x-circle" size={16} />
          {body}
        </div>
      ) : (
        body
      )}
    </div>
  );
}
