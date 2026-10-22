import { useState, useEffect, useCallback, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { Save, Settings2, Palette, Globe, Download } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { getSettings, saveSettings, applyThemeMode, applyFont, downloadOptions, FONT_OPTIONS, type Settings as SettingsType, type FontFamily } from "@/lib/settings";
import { themes, applyTheme } from "@/lib/themes";
import { LOCALES, availableLanguages, setLanguage, t as translate, useI18n, type MessageKey } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { FILENAME_PRESETS, FOLDER_PRESETS } from "@/lib/downloadPresets";
import { SourceStatus } from "@/components/SourceStatus";
import { DefaultDownloadDir, FindFFmpeg, PreviewDownloadPath, SelectDownloadFolder, SelectFFmpeg } from "../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";

const SECTIONS = [
    { id: "general", label: "settings.general", Icon: Settings2 },
    { id: "downloads", label: "settings.downloads", Icon: Download },
    { id: "appearance", label: "settings.appearance", Icon: Palette },
    { id: "network", label: "settings.network", Icon: Globe },
] as const satisfies readonly { id: string; label: MessageKey; Icon: unknown }[];
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
    return (<div className="flex items-center justify-between gap-6 px-4 py-4 border-b last:border-b-0">
      <div className="space-y-1 min-w-0">
        <Label htmlFor={id}>{label}</Label>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      <div className={wide ? "w-80 shrink-0" : "w-48 shrink-0"}>{children}</div>
    </div>);
}

// Radix Select rejects an empty item value, so the "no subfolder" preset gets a stand-in.
const NONE = "none";
const CUSTOM = "custom";

function TemplateSelect({ id, value, presets, onChange }: { id: string; value: string; presets: readonly string[]; onChange: (value: string) => void }) {
    const { t } = useI18n();
    const [custom, setCustom] = useState(!presets.includes(value));
    return (<div className="space-y-2">
      <Select value={custom ? CUSTOM : value || NONE} onValueChange={(next) => {
          setCustom(next === CUSTOM);
          if (next !== CUSTOM) onChange(next === NONE ? "" : next);
      }}>
        <SelectTrigger id={id}>
          <SelectValue/>
        </SelectTrigger>
        <SelectContent>
          {presets.map((preset) => (<SelectItem key={preset} value={preset || NONE}>{preset || t("download.noSubfolder")}</SelectItem>))}
          <SelectItem value={CUSTOM}>{t("download.custom")}</SelectItem>
        </SelectContent>
      </Select>
      {custom && <Input value={value} onChange={(e) => onChange(e.target.value)} className="font-mono"/>}
    </div>);
}

function DownloadsSection({ settings, set }: { settings: SettingsType; set: (partial: Partial<SettingsType>) => void }) {
    const { t } = useI18n();
    const [defaultDir, setDefaultDir] = useState("");
    const [preview, setPreview] = useState("");
    const [ffmpegFound, setFfmpegFound] = useState<string | null>(null);
    // The limit is typed as text so it can be cleared while editing; only a valid value reaches the settings.
    const [limitText, setLimitText] = useState(String(settings.rateGuardLimit));
    useEffect(() => {
        DefaultDownloadDir().then(setDefaultDir, (error) => console.error("Failed to get the default download folder:", error));
    }, []);
    const optionsKey = JSON.stringify(downloadOptions(settings));
    useEffect(() => {
        let stale = false;
        PreviewDownloadPath(JSON.parse(optionsKey)).then((path) => { if (!stale) setPreview(path); }, () => { if (!stale) setPreview(""); });
        return () => { stale = true; };
    }, [optionsKey]);
    const needsFFmpeg = settings.downloadFormat !== "original";
    useEffect(() => {
        if (!needsFFmpeg) return;
        let stale = false;
        FindFFmpeg(settings.ffmpegPath).then((path) => { if (!stale) setFfmpegFound(path); }, () => { if (!stale) setFfmpegFound(""); });
        return () => { stale = true; };
    }, [needsFFmpeg, settings.ffmpegPath]);
    const browse = async (pick: () => Promise<string>, apply: (path: string) => void) => {
        const path = await pick();
        if (path) apply(path);
    };
    return (<>
    <Row id="download-path" label={t("settings.downloadFolder")} description={t("settings.downloadFolderDesc")} wide>
      <div className="flex gap-2">
        <Input id="download-path" value={settings.downloadPath} onChange={(e) => set({ downloadPath: e.target.value })} placeholder={defaultDir}/>
        <Button type="button" variant="outline" onClick={() => browse(() => SelectDownloadFolder(settings.downloadPath || defaultDir), (path) => set({ downloadPath: path }))}>{t("settings.browse")}</Button>
      </div>
    </Row>
    <Row id="download-source" label={t("settings.source")} description={t("settings.sourceDesc")} wide>
      <div className="space-y-2">
        <Select value={settings.downloadSource} onValueChange={(value: SettingsType["downloadSource"]) => set({ downloadSource: value })}>
          <SelectTrigger id="download-source">
            <SelectValue/>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="auto">{t("download.sourceAuto")}</SelectItem>
            <SelectItem value="lucida">lucida</SelectItem>
            <SelectItem value="doubledouble">doubledouble</SelectItem>
          </SelectContent>
        </Select>
        <SourceStatus/>
      </div>
    </Row>
    <Row id="download-format" label={t("settings.format")}>
      <Select value={settings.downloadFormat} onValueChange={(value: SettingsType["downloadFormat"]) => set({ downloadFormat: value })}>
        <SelectTrigger id="download-format">
          <SelectValue/>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="original">{t("download.formatOriginal")}</SelectItem>
          <SelectItem value="mp3-320">MP3 320 kbps</SelectItem>
          <SelectItem value="m4a-aac-320">M4A AAC 320 kbps</SelectItem>
        </SelectContent>
      </Select>
    </Row>
    {needsFFmpeg && (<Row id="ffmpeg-path" label={t("settings.ffmpeg")} description={t("settings.ffmpegDesc")} wide>
      <div className="space-y-2">
        <div className="flex gap-2">
          <Input id="ffmpeg-path" value={settings.ffmpegPath} onChange={(e) => set({ ffmpegPath: e.target.value })} placeholder={t("download.ffmpegAuto")}/>
          <Button type="button" variant="outline" onClick={() => browse(SelectFFmpeg, (path) => set({ ffmpegPath: path }))}>{t("settings.browse")}</Button>
        </div>
        {ffmpegFound === null ? null : ffmpegFound
            ? <p className="text-xs text-muted-foreground break-all">{t("download.ffmpegFound", { path: ffmpegFound })}</p>
            : <p className="text-xs text-destructive">
                {t("download.ffmpegMissing")}{" "}
                <button type="button" className="underline" onClick={() => BrowserOpenURL("https://ffmpeg.org/download.html")}>{t("download.ffmpegGet")}</button>
              </p>}
      </div>
    </Row>)}
    <Row id="folder-template" label={t("settings.folderTemplate")} description={t("settings.templateDesc")} wide>
      <TemplateSelect id="folder-template" value={settings.folderTemplate} presets={FOLDER_PRESETS} onChange={(value) => set({ folderTemplate: value })}/>
    </Row>
    <Row id="filename-template" label={t("settings.filenameTemplate")} description={t("settings.templateDesc")} wide>
      <TemplateSelect id="filename-template" value={settings.filenameTemplate} presets={FILENAME_PRESETS} onChange={(value) => set({ filenameTemplate: value })}/>
    </Row>
    <Row id="playlist-folder" label={t("settings.playlistFolder")} description={t("settings.playlistFolderDesc")}>
      <Switch id="playlist-folder" checked={settings.playlistFolder} onCheckedChange={(checked) => set({ playlistFolder: checked })}/>
    </Row>
    <Row id="rate-guard" label={t("settings.rateGuard")} description={t("settings.rateGuardDesc")}>
      <div className="flex items-center gap-3">
        <Switch id="rate-guard" checked={settings.rateGuard} onCheckedChange={(checked) => set({ rateGuard: checked })}/>
        {settings.rateGuard && <Input type="number" min={1} step={1} value={limitText} className="w-20" onChange={(e) => {
            setLimitText(e.target.value);
            if (/^[1-9]\d*$/.test(e.target.value)) set({ rateGuardLimit: Number(e.target.value) });
        }} onBlur={() => setLimitText(String(settings.rateGuardLimit))}/>}
      </div>
    </Row>
    <Row id="download-preview" label={t("settings.preview")} wide>
      <p className="text-xs font-mono text-muted-foreground break-all">{preview}</p>
    </Row>
    </>);
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
    const set = (partial: Partial<SettingsType>) => setTempSettings((prev) => ({ ...prev, ...partial }));
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

      {/* Same tab strip and bordered table as the History page. */}
      <div className="flex gap-2 border-b shrink-0">
        {SECTIONS.map(({ id, label, Icon }) => (<Button key={id} variant={section === id ? "default" : "ghost"} size="sm" className="rounded-b-none" onClick={() => setSection(id)}>
          <Icon className="h-4 w-4"/>
          {t(label)}
        </Button>))}
      </div>

      <div className="rounded-md border overflow-hidden">
        <div className="h-10 px-4 flex items-center border-b bg-muted/50 text-xs font-medium uppercase text-muted-foreground">
          {t(SECTIONS.find((s) => s.id === section)!.label)}
        </div>
        <div>
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

          {section === "downloads" && <DownloadsSection settings={tempSettings} set={set}/>}

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
