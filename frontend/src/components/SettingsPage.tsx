import { useState, useEffect, useCallback, useRef, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { Save, Settings2, Palette, Globe, Download } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { getSettings, saveSettings, applyThemeMode, applyFont, FONT_OPTIONS, type Settings as SettingsType, type FontFamily } from "@/lib/settings";
import { themes, applyTheme } from "@/lib/themes";
import { LOCALES, availableLanguages, setLanguage, t as translate, useI18n, type MessageKey } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { openFFmpegInstall } from "@/lib/ffmpeg-install";
import { FILENAME_PRESETS, FOLDER_PRESETS } from "@/lib/downloadPresets";
import { SourceStatus } from "@/components/SourceStatus";
import { siteName } from "@/lib/download-format";
import { DefaultDownloadDir, FindFFmpeg, GetSourceStatus, SelectDownloadFolder, SelectFFmpeg } from "../../wailsjs/go/main/App";
import { BrowserOpenURL, EventsOn } from "../../wailsjs/runtime/runtime";

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

// Every control sits in one right-hand column of the same width, so their edges line up.
function Row({ id, label, description, children }: { id: string; label: string; description?: string; children: ReactNode }) {
    return (<div className="flex items-center justify-between gap-6 px-4 py-4 border-b last:border-b-0">
      <div className="space-y-1 min-w-0">
        <Label htmlFor={id}>{label}</Label>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      <div className="w-72 shrink-0 flex flex-col items-end [&_[data-slot=select-trigger]]:w-full">{children}</div>
    </div>);
}

function Block({ title, children }: { title: string; children: ReactNode }) {
    return (<section className="rounded-md border overflow-hidden">
      <h2 className="h-10 px-4 flex items-center border-b bg-muted/50 text-xs font-medium uppercase text-muted-foreground">{title}</h2>
      <div>{children}</div>
    </section>);
}

// Radix Select rejects an empty item value, so the "no subfolder" preset gets a stand-in.
const NONE = "none";
const CUSTOM = "custom";
// The history keeps 200 items.
const MAX_RECENT = 200;

// Typed as text so it can be cleared while editing; only a whole number from 1 to max reaches onChange.
function LimitInput({ id, value, max, label, onChange }: { id?: string; value: number; max: number; label: string; onChange: (value: number) => void }) {
    const [text, setText] = useState(String(value));
    return (<Input id={id} type="number" min={1} max={max} step={1} value={text} className="w-20" aria-label={label} onChange={(e) => {
        setText(e.target.value);
        const n = Number(e.target.value);
        if (/^[1-9]\d*$/.test(e.target.value) && n <= max) onChange(n);
    }} onBlur={() => setText(String(value))}/>);
}

// Long paths do not fit the field, so the whole one shows on hover.
function PathInput({ id, value, placeholder, hint, onChange }: { id: string; value: string; placeholder: string; hint: string; onChange: (value: string) => void }) {
    return (<Tooltip delayDuration={300}>
      <TooltipTrigger asChild>
        <Input id={id} value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder}/>
      </TooltipTrigger>
      {hint && <TooltipContent side="bottom" className="max-w-md break-all">{hint}</TooltipContent>}
    </Tooltip>);
}

function TemplateSelect({ id, value, presets, onChange }: { id: string; value: string; presets: readonly string[]; onChange: (value: string) => void }) {
    const { t } = useI18n();
    const [custom, setCustom] = useState(!presets.includes(value));
    return (<div className="w-full space-y-2">
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
      {custom && (<>
        <Input value={value} onChange={(e) => onChange(e.target.value)} className="font-mono"/>
        <p className="text-xs text-muted-foreground">{t("settings.templateDesc")}</p>
      </>)}
    </div>);
}

function DownloadsSection({ settings, set }: { settings: SettingsType; set: (partial: Partial<SettingsType>) => void }) {
    const { t } = useI18n();
    const [defaultDir, setDefaultDir] = useState("");
    const [ffmpegFound, setFfmpegFound] = useState<string | null>(null);
    const [downloaders, setDownloaders] = useState<{ name: string; label: string }[]>([]);
    // The status lists the enabled downloaders only, so it is the list of sites the user can pick.
    useEffect(() => {
        const load = () => GetSourceStatus().then((list) => setDownloaders(list ?? []), (error) => console.error("Failed to load the download sites:", error));
        void load();
        return EventsOn("plugins:changed", () => void load());
    }, []);
    useEffect(() => {
        DefaultDownloadDir().then(setDefaultDir, (error) => console.error("Failed to get the default download folder:", error));
    }, []);
    const needsFFmpeg = settings.downloadFormat !== "original";
    useEffect(() => {
        if (!needsFFmpeg) return;
        let stale = false;
        setFfmpegFound(null);
        FindFFmpeg(settings.ffmpegPath).then((path) => { if (!stale) setFfmpegFound(path); }, () => { if (!stale) setFfmpegFound(""); });
        return () => { stale = true; };
    }, [needsFFmpeg, settings.ffmpegPath]);
    // Choosing MP3 or M4A without FFmpeg is the moment to offer the install.
    const offerInstall = (format: string) => {
        if (format === "original") return;
        FindFFmpeg(settings.ffmpegPath).then((found) => { if (!found) openFFmpegInstall((ffmpegPath) => set({ ffmpegPath })); }, () => {});
    };
    const browse = async (pick: () => Promise<string>, apply: (path: string) => void) => {
        try {
            const path = await pick();
            if (path) apply(path);
        }
        catch (error) { toast.error(String(error)); }
    };
    return (<div className="space-y-4">
    <Block title={t("settings.blockSaving")}>
      <Row id="download-path" label={t("settings.downloadFolder")} description={t("settings.downloadFolderDesc")}>
        <div className="w-full flex gap-2">
          <PathInput id="download-path" value={settings.downloadPath} onChange={(downloadPath) => set({ downloadPath })} placeholder={defaultDir} hint={settings.downloadPath || defaultDir}/>
          <Button type="button" variant="outline" onClick={() => browse(() => SelectDownloadFolder(settings.downloadPath || defaultDir), (path) => set({ downloadPath: path }))}>{t("settings.browse")}</Button>
        </div>
      </Row>
      <Row id="download-format" label={t("settings.format")} description={t("settings.formatDesc")}>
        <Select value={settings.downloadFormat} onValueChange={(value: SettingsType["downloadFormat"]) => { set({ downloadFormat: value }); offerInstall(value); }}>
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
      {needsFFmpeg && (<Row id="ffmpeg-path" label={t("settings.ffmpeg")} description={t("settings.ffmpegDesc")}>
        <div className="w-full space-y-2">
          <div className="flex gap-2">
            <PathInput id="ffmpeg-path" value={settings.ffmpegPath} onChange={(ffmpegPath) => set({ ffmpegPath })}
                placeholder={!settings.ffmpegPath && ffmpegFound ? ffmpegFound : t("download.ffmpegAuto")}
                hint={settings.ffmpegPath || (ffmpegFound ? t("download.ffmpegFound", { path: ffmpegFound }) : "")}/>
            <Button type="button" variant="outline" onClick={() => browse(SelectFFmpeg, (path) => set({ ffmpegPath: path }))}>{t("settings.browse")}</Button>
          </div>
          {ffmpegFound === "" && (<p className="text-xs text-destructive">
                  {t("download.ffmpegMissing")}{" "}
                  <button type="button" className="underline" onClick={() => BrowserOpenURL("https://ffmpeg.org/download.html")}>{t("download.ffmpegGet")}</button>
                </p>)}
        </div>
      </Row>)}
    </Block>
    <Block title={t("settings.blockNaming")}>
      <Row id="folder-template" label={t("settings.folderTemplate")} description={t("settings.folderTemplateDesc")}>
        <TemplateSelect id="folder-template" value={settings.folderTemplate} presets={FOLDER_PRESETS} onChange={(value) => set({ folderTemplate: value })}/>
      </Row>
      <Row id="filename-template" label={t("settings.filenameTemplate")} description={t("settings.filenameTemplateDesc")}>
        <TemplateSelect id="filename-template" value={settings.filenameTemplate} presets={FILENAME_PRESETS} onChange={(value) => set({ filenameTemplate: value })}/>
      </Row>
      <Row id="playlist-folder" label={t("settings.playlistFolder")} description={t("settings.playlistFolderDesc")}>
        <Switch id="playlist-folder" checked={settings.playlistFolder} onCheckedChange={(checked) => set({ playlistFolder: checked })}/>
      </Row>
    </Block>
    <Block title={t("settings.blockSource")}>
      <Row id="download-source" label={t("settings.source")} description={t("settings.sourceDesc")}>
        <Select value={settings.downloadSource} onValueChange={(value) => set({ downloadSource: value })}>
          <SelectTrigger id="download-source">
            <SelectValue/>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="auto">{t("download.sourceAuto")}</SelectItem>
            {downloaders.map((site) => <SelectItem key={site.name} value={site.name}>{siteName(site)}</SelectItem>)}
            {/* A downloader that is switched off since it was chosen stays visible until the user picks another. */}
            {settings.downloadSource !== "auto" && !downloaders.some((site) => site.name === settings.downloadSource) && <SelectItem value={settings.downloadSource}>{settings.downloadSource}</SelectItem>}
          </SelectContent>
        </Select>
      </Row>
      <Row id="site-status" label={t("settings.siteStatus")} description={t("settings.siteStatusDesc")}>
        <div className="w-full"><SourceStatus withAction={false}/></div>
      </Row>
    </Block>
    </div>);
}

interface SettingsPageProps {
    onUnsavedChangesChange?: (hasUnsavedChanges: boolean) => void;
    onResetRequest?: (resetFn: () => void) => void;
    /** Hands over a save the unsaved-changes dialog can run; it resolves to whether the save worked. */
    onSaveRequest?: (saveFn: () => Promise<boolean>) => void;
}

export function SettingsPage({ onUnsavedChangesChange, onResetRequest, onSaveRequest }: SettingsPageProps) {
    const { t } = useI18n();
    const [savedSettings, setSavedSettings] = useState<SettingsType>(getSettings());
    const [tempSettings, setTempSettings] = useState<SettingsType>(savedSettings);
    const [isDark, setIsDark] = useState(document.documentElement.classList.contains('dark'));
    const [isSaving, setIsSaving] = useState(false);
    const [section, setSection] = useState<SectionId>("general");
    // Remounts the Downloads section on reset, so its local state follows the restored settings.
    const [resetCount, setResetCount] = useState(0);
    const hasUnsavedChanges = JSON.stringify(savedSettings) !== JSON.stringify(tempSettings);
    const resetToSaved = useCallback(() => {
        const freshSavedSettings = getSettings();
        flushSync(() => {
            setTempSettings(freshSavedSettings);
            setResetCount((count) => count + 1);
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
            return true;
        }
        catch (error) {
            // tempSettings stays as typed, so a bad proxy can be corrected in place.
            toast.error(translate("settings.saveFailed", { error: String(error) }));
            return false;
        }
        finally { setIsSaving(false); }
    };
    const saveRef = useRef(handleSave);
    saveRef.current = handleSave;
    useEffect(() => { onSaveRequest?.(() => saveRef.current()); }, [onSaveRequest]);
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

      {section === "downloads" ? <DownloadsSection key={resetCount} settings={tempSettings} set={set}/> : (
        <Block title={t(SECTIONS.find((s) => s.id === section)!.label)}>
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
            <Row id="recent-limit" label={t("settings.recentLimit")} description={t("settings.recentLimitDesc")}>
              <LimitInput key={resetCount} id="recent-limit" value={tempSettings.recentLimit} max={MAX_RECENT} label={t("settings.recentLimit")} onChange={(limit) => set({ recentLimit: limit })}/>
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
            <Row id="proxy" label={t("settings.proxy")} description={t("settings.proxyDesc")}>
              <Input id="proxy" value={tempSettings.proxy} onChange={(e) => setTempSettings((prev) => ({ ...prev, proxy: e.target.value }))} placeholder={t("settings.proxyPlaceholder")}/>
            </Row>
          )}
        </Block>)}
    </div>);
}
