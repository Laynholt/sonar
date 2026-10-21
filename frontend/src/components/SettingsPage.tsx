import { useState, useEffect, useCallback, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { getSettings, saveSettings, applyThemeMode, applyFont, FONT_OPTIONS, type Settings as SettingsType, type FontFamily } from "@/lib/settings";
import { themes, applyTheme } from "@/lib/themes";
import { LOCALES, availableLanguages, setLanguage, t as translate, useI18n, type MessageKey } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";

const SECTIONS = [
    { id: "general", label: "settings.general" },
    { id: "appearance", label: "settings.appearance" },
    { id: "network", label: "settings.network" },
] as const satisfies readonly { id: string; label: MessageKey }[];
// Accent names are plain colour words, so they follow the language; unknown themes keep their own label.
const THEME_LABELS: Record<string, MessageKey> = {
    amber: "theme.amber",
    blue: "theme.blue",
    cyan: "theme.cyan",
    emerald: "theme.emerald",
    fuchsia: "theme.fuchsia",
    green: "theme.green",
    indigo: "theme.indigo",
    lime: "theme.lime",
    neutral: "theme.neutral",
    orange: "theme.orange",
    pink: "theme.pink",
    purple: "theme.purple",
    red: "theme.red",
    rose: "theme.rose",
    sky: "theme.sky",
    teal: "theme.teal",
    violet: "theme.violet",
    yellow: "theme.yellow",
};
type SectionId = typeof SECTIONS[number]["id"];

function Row({ id, label, description, children, wide }: { id: string; label: string; description?: string; children: ReactNode; wide?: boolean }) {
    return (<div className="flex items-center justify-between gap-6 py-4 border-b last:border-b-0">
      <div className="space-y-1 min-w-0">
        <Label htmlFor={id}>{label}</Label>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      <div className={wide ? "w-80 shrink-0" : "w-48 shrink-0"}>{children}</div>
    </div>);
}

interface SettingsPageProps {
    onUnsavedChangesChange?: (hasUnsavedChanges: boolean) => void;
    onResetRequest?: (resetFn: () => void) => void;
}

export function SettingsPage({ onUnsavedChangesChange, onResetRequest }: SettingsPageProps) {
    const { t } = useI18n();
    const [savedSettings, setSavedSettings] = useState<SettingsType>(getSettings());
    const [tempSettings, setTempSettings] = useState<SettingsType>(savedSettings);
    const [isDark, setIsDark] = useState(document.documentElement.classList.contains('dark'));
    const [isSaving, setIsSaving] = useState(false);
    const [section, setSection] = useState<SectionId>("general");
    const hasUnsavedChanges = JSON.stringify(savedSettings) !== JSON.stringify(tempSettings);
    const resetToSaved = useCallback(() => {
        const freshSavedSettings = getSettings();
        flushSync(() => {
            setTempSettings(freshSavedSettings);
            setIsDark(document.documentElement.classList.contains('dark'));
        });
    }, []);
    useEffect(() => {
        if (onResetRequest) {
            onResetRequest(resetToSaved);
        }
    }, [onResetRequest, resetToSaved]);
    useEffect(() => {
        onUnsavedChangesChange?.(hasUnsavedChanges);
    }, [hasUnsavedChanges, onUnsavedChangesChange]);
    useEffect(() => {
        applyThemeMode(savedSettings.themeMode);
        applyTheme(savedSettings.theme);
        const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
        const handleChange = () => {
            if (savedSettings.themeMode === "auto") {
                applyThemeMode("auto");
                applyTheme(savedSettings.theme);
            }
        };
        mediaQuery.addEventListener("change", handleChange);
        return () => mediaQuery.removeEventListener("change", handleChange);
    }, [savedSettings.themeMode, savedSettings.theme]);
    useEffect(() => {
        applyThemeMode(tempSettings.themeMode);
        applyTheme(tempSettings.theme);
        applyFont(tempSettings.fontFamily);
        setLanguage(tempSettings.language);
        setTimeout(() => {
            setIsDark(document.documentElement.classList.contains('dark'));
        }, 0);
    }, [tempSettings.themeMode, tempSettings.theme, tempSettings.fontFamily, tempSettings.language]);
    const handleSave = async () => {
        setIsSaving(true);
        try {
            await saveSettings(tempSettings);
            setSavedSettings(getSettings());
            toast.success(translate("settings.saved"));
        }
        catch (error) {
            // tempSettings stays as typed, so a bad proxy can be corrected in place.
            toast.error(translate("settings.saveFailed", { error: String(error) }));
        }
        finally { setIsSaving(false); }
    };
    return (<div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">{t("nav.settings")}</h1>
        <Button onClick={handleSave} disabled={isSaving} className="gap-1.5">
          <Save className="h-4 w-4"/>
          {t("settings.save")}
        </Button>
      </div>

      <div className="flex gap-8">
        <nav className="flex w-40 shrink-0 flex-col gap-1">
          {SECTIONS.map(({ id, label }) => (<Button key={id} variant={section === id ? "secondary" : "ghost"} className="justify-start" onClick={() => setSection(id)}>{t(label)}</Button>))}
        </nav>

        <div className="min-w-0 flex-1">
          {section === "general" && (<>
            <Row id="language" label={t("settings.language")} description={t("settings.languageDesc")}>
              <Select value={Object.hasOwn(LOCALES, tempSettings.language) ? tempSettings.language : "auto"} onValueChange={(value) => setTempSettings((prev) => ({ ...prev, language: value }))}>
                <SelectTrigger id="language">
                  <SelectValue placeholder={t("settings.languagePlaceholder")}/>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">{t("settings.system")}</SelectItem>
                  {availableLanguages().map(({ code, name }) => (<SelectItem key={code} value={code}>{name}</SelectItem>))}
                </SelectContent>
              </Select>
            </Row>
            <Row id="sfx-enabled" label={t("settings.sfx")} description={t("settings.sfxDesc")}>
              <Switch id="sfx-enabled" checked={tempSettings.sfxEnabled} onCheckedChange={(checked) => setTempSettings(prev => ({ ...prev, sfxEnabled: checked }))}/>
            </Row>
          </>)}

          {section === "appearance" && (<>
            <Row id="theme-mode" label={t("settings.mode")} description={t("settings.modeDesc")}>
              <Select value={tempSettings.themeMode} onValueChange={(value: "auto" | "light" | "dark") => setTempSettings((prev) => ({ ...prev, themeMode: value }))}>
                <SelectTrigger id="theme-mode">
                  <SelectValue placeholder={t("settings.modePlaceholder")}/>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">{t("settings.auto")}</SelectItem>
                  <SelectItem value="light">{t("settings.light")}</SelectItem>
                  <SelectItem value="dark">{t("settings.dark")}</SelectItem>
                </SelectContent>
              </Select>
            </Row>
            <Row id="theme" label={t("settings.accent")} description={t("settings.accentDesc")}>
              <Select value={tempSettings.theme} onValueChange={(value) => setTempSettings((prev) => ({ ...prev, theme: value }))}>
                <SelectTrigger id="theme">
                  <SelectValue placeholder={t("settings.accentPlaceholder")}/>
                </SelectTrigger>
                <SelectContent>
                  {themes.map((theme) => (<SelectItem key={theme.name} value={theme.name}>
                    <span className="flex items-center gap-2">
                      <span className="w-3 h-3 rounded-full border border-border" style={{
                      backgroundColor: isDark ? theme.cssVars.dark.primary : theme.cssVars.light.primary
                  }}/>
                      {THEME_LABELS[theme.name] ? t(THEME_LABELS[theme.name]) : theme.label}
                    </span>
                  </SelectItem>))}
                </SelectContent>
              </Select>
            </Row>
            <Row id="font" label={t("settings.font")} description={t("settings.fontDesc")}>
              <Select value={tempSettings.fontFamily} onValueChange={(value: FontFamily) => setTempSettings((prev) => ({ ...prev, fontFamily: value }))}>
                <SelectTrigger id="font">
                  <SelectValue placeholder={t("settings.fontPlaceholder")}/>
                </SelectTrigger>
                <SelectContent>
                  {FONT_OPTIONS.map((font) => (<SelectItem key={font.value} value={font.value}>
                    <span style={{ fontFamily: font.fontFamily }}>{font.label}</span>
                  </SelectItem>))}
                </SelectContent>
              </Select>
            </Row>
          </>)}

          {section === "network" && (
            <Row id="proxy" label={t("settings.proxy")} wide description={t("settings.proxyDesc")}>
              <Input id="proxy" value={tempSettings.proxy} onChange={(e) => setTempSettings((prev) => ({ ...prev, proxy: e.target.value }))} placeholder={t("settings.proxyPlaceholder")}/>
            </Row>
          )}
        </div>
      </div>
    </div>);
}
