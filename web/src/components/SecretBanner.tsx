import { useState } from 'react';

interface Props {
  title: string;
  secret: string;
  hint?: string;
  onDismiss: () => void;
}

/** Shows a credential that cannot be retrieved again. */
export function SecretBanner({ title, secret, hint, onDismiss }: Props) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch {
      setCopied(false); // clipboard blocked (non-HTTPS); the value is selectable below
    }
  };

  return (
    <div className="banner banner--secret" role="alert">
      <strong>{title}</strong>
      <p>{hint ?? 'Copy it now. It is not stored and cannot be shown again.'}</p>
      <code className="secret-value">{secret}</code>
      <div className="row-actions">
        <button className="btn btn--sm" onClick={copy}>{copied ? 'Copied' : 'Copy'}</button>
        <button className="btn btn--sm" onClick={onDismiss}>I have saved it</button>
      </div>
    </div>
  );
}
