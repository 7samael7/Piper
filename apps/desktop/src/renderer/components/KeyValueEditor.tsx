interface KeyValueEditorProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  secret?: boolean;
}

export function KeyValueEditor({ label, value, onChange, secret = false }: KeyValueEditorProps) {
  return (
    <label className="field-stack">
      <span>{label}</span>
      <textarea
        spellCheck={false}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className={secret ? "secret-textarea" : ""}
        placeholder={secret ? "TOKEN=..." : "KEY=value"}
      />
    </label>
  );
}

export function parseKeyValueText(value: string): Record<string, string> {
  const entries: Record<string, string> = {};
  for (const rawLine of value.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    const index = line.indexOf("=");
    if (index <= 0) {
      continue;
    }
    entries[line.slice(0, index).trim()] = line.slice(index + 1);
  }
  return entries;
}
