import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Row } from "@/components/SettingsRows";
import { useI18n, type MessageKey } from "@/lib/i18n";
import { textIn } from "@/lib/plugins";
import type { plugin } from "../../wailsjs/go/models";

// The host adds these two to a downloader with a guard, unlabelled: the app names them from its own copy.
const GUARD_LABELS: Record<string, MessageKey> = { $guardLimit: "plugins.guardLimit", $guardWait: "plugins.guardWait" };

export interface PluginSettingsProps {
    fields: plugin.Field[];
    values: Record<string, unknown>;
    lang: string;
    onChange: (key: string, value: unknown) => void;
    onAction: (action: string) => void;
}

// Typed as text so it can be cleared while editing; only a number reaches onChange.
function NumberInput({ id, value, onChange }: { id: string; value: unknown; onChange: (value: number) => void }) {
    const [text, setText] = useState(String(value ?? ""));
    return (<Input id={id} type="number" value={text} className="w-24" onChange={(e) => {
        setText(e.target.value);
        if (e.target.value.trim() !== "" && Number.isFinite(Number(e.target.value))) onChange(Number(e.target.value));
    }} onBlur={() => setText(String(value ?? ""))}/>);
}

/** A plugin's settings fields: text, number, switch, select, button and note. */
export function PluginSettings({ fields, values, lang, onChange, onAction }: PluginSettingsProps) {
    const { t } = useI18n();
    const base = useId();
    return (<div>
      {fields.map((field) => {
          const id = `${base}-${field.key}`;
          const label = GUARD_LABELS[field.key] ? t(GUARD_LABELS[field.key]) : textIn(field.label, lang) || field.key;
          const hint = textIn(field.hint, lang);
          const value = values[field.key] ?? field.default;
          switch (field.type) {
              case "note":
                  return (<p key={field.key} className="px-4 py-4 border-b last:border-b-0 text-xs text-muted-foreground">{label}{hint && <><br/>{hint}</>}</p>);
              case "button":
                  return (<div key={field.key} className="px-4 py-4 border-b last:border-b-0 space-y-1">
                    <Button type="button" variant="outline" size="sm" onClick={() => onAction(field.action)}>{label}</Button>
                    {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
                  </div>);
          }
          return (<Row key={field.key} id={id} label={label} description={hint}>
            {field.type === "switch" ? <Switch id={id} checked={!!value} onCheckedChange={(on) => onChange(field.key, on)}/>
                : field.type === "number" ? <NumberInput id={id} value={value} onChange={(n) => onChange(field.key, n)}/>
                : field.type === "select" ? (<Select value={String(value ?? "")} onValueChange={(next) => onChange(field.key, field.options?.find((o) => String(o.value) === next)?.value ?? next)}>
                    <SelectTrigger id={id}><SelectValue/></SelectTrigger>
                    <SelectContent>
                      {(field.options ?? []).map((o) => (<SelectItem key={String(o.value)} value={String(o.value)}>
                        {field.key === "$guardWait" ? t("plugins.minutes", { minutes: String(o.value) }) : textIn(o.label, lang) || String(o.value)}
                      </SelectItem>))}
                    </SelectContent>
                  </Select>)
                : <Input id={id} type="text" value={String(value ?? "")} onChange={(e) => onChange(field.key, e.target.value)}/>}
          </Row>);
      })}
    </div>);
}
