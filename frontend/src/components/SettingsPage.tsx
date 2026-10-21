import { useState, useEffect, useCallback } from "react";
import { flushSync } from "react-dom";
import { Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { getSettings, saveSettings, applyThemeMode, applyFont, FONT_OPTIONS, type Settings as SettingsType, type FontFamily } from "@/lib/settings";
import { themes, applyTheme } from "@/lib/themes";
import { toastWithSound as toast } from "@/lib/toast-with-sound";

interface SettingsPageProps {
    onUnsavedChangesChange?: (hasUnsavedChanges: boolean) => void;
    onResetRequest?: (resetFn: () => void) => void;
}

export function SettingsPage({ onUnsavedChangesChange, onResetRequest }: SettingsPageProps) {
    const [savedSettings, setSavedSettings] = useState<SettingsType>(getSettings());
    const [tempSettings, setTempSettings] = useState<SettingsType>(savedSettings);
    const [isDark, setIsDark] = useState(document.documentElement.classList.contains('dark'));
    const [isSaving, setIsSaving] = useState(false);
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
        setTimeout(() => {
            setIsDark(document.documentElement.classList.contains('dark'));
        }, 0);
    }, [tempSettings.themeMode, tempSettings.theme, tempSettings.fontFamily]);
    const handleSave = async () => {
        setIsSaving(true);
        try {
            await saveSettings(tempSettings);
            setSavedSettings(getSettings());
            toast.success("Settings saved");
        }
        catch (error) {
            // tempSettings stays as typed, so a bad proxy can be corrected in place.
            toast.error(`Failed to save settings: ${error}`);
        }
        finally { setIsSaving(false); }
    };
    return (<div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Settings</h1>
        <Button onClick={handleSave} disabled={isSaving} className="gap-1.5">
          <Save className="h-4 w-4"/>
          Save Changes
        </Button>
      </div>

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Appearance</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="theme-mode">Mode</Label>
            <Select value={tempSettings.themeMode} onValueChange={(value: "auto" | "light" | "dark") => setTempSettings((prev) => ({ ...prev, themeMode: value }))}>
              <SelectTrigger id="theme-mode">
                <SelectValue placeholder="Select theme mode"/>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">Auto</SelectItem>
                <SelectItem value="light">Light</SelectItem>
                <SelectItem value="dark">Dark</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="theme">Accent</Label>
            <Select value={tempSettings.theme} onValueChange={(value) => setTempSettings((prev) => ({ ...prev, theme: value }))}>
              <SelectTrigger id="theme">
                <SelectValue placeholder="Select a theme"/>
              </SelectTrigger>
              <SelectContent>
                {themes.map((theme) => (<SelectItem key={theme.name} value={theme.name}>
                  <span className="flex items-center gap-2">
                    <span className="w-3 h-3 rounded-full border border-border" style={{
                    backgroundColor: isDark ? theme.cssVars.dark.primary : theme.cssVars.light.primary
                }}/>
                    {theme.label}
                  </span>
                </SelectItem>))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="font">Font</Label>
            <Select value={tempSettings.fontFamily} onValueChange={(value: FontFamily) => setTempSettings((prev) => ({ ...prev, fontFamily: value }))}>
              <SelectTrigger id="font">
                <SelectValue placeholder="Select a font"/>
              </SelectTrigger>
              <SelectContent>
                {FONT_OPTIONS.map((font) => (<SelectItem key={font.value} value={font.value}>
                  <span style={{ fontFamily: font.fontFamily }}>{font.label}</span>
                </SelectItem>))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex items-center gap-3 md:pt-6">
            <Switch id="sfx-enabled" checked={tempSettings.sfxEnabled} onCheckedChange={(checked) => setTempSettings(prev => ({ ...prev, sfxEnabled: checked }))}/>
            <Label htmlFor="sfx-enabled" className="cursor-pointer text-sm font-normal">Sound Effects</Label>
          </div>
        </div>
      </section>

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Network</h2>
        <div className="space-y-2">
          <Label htmlFor="proxy">Proxy</Label>
          <Input id="proxy" value={tempSettings.proxy} onChange={(e) => setTempSettings((prev) => ({ ...prev, proxy: e.target.value }))} placeholder="Auto (system proxy)"/>
          <p className="text-xs text-muted-foreground">
            Leave empty to use HTTPS_PROXY or the system proxy. Examples: http://127.0.0.1:10808, socks5://127.0.0.1:10808
          </p>
        </div>
      </section>
    </div>);
}
