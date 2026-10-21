import { useEffect, useState } from "react";
import { X, Minus, Maximize } from "lucide-react";
import { Environment, WindowMinimise, WindowToggleMaximise, Quit } from "../../wailsjs/runtime/runtime";

const noDrag = { "--wails-draggable": "no-drag" } as React.CSSProperties;
const btn = "w-8 h-7 flex items-center justify-center hover:bg-muted transition-colors rounded";

export function TitleBar() {
    // macOS keeps the system traffic lights (TitleBarHiddenInset), so no window buttons of our own there.
    const [platform, setPlatform] = useState<string | null>(null);
    useEffect(() => {
        void Environment().then((env) => setPlatform(env.platform)).catch(() => setPlatform(""));
    }, []);
    const isMac = platform === "darwin";
    return (<>
      <div className="fixed top-0 left-14 right-0 h-10 z-40 bg-background/80 backdrop-blur-sm" style={{ "--wails-draggable": "drag", paddingLeft: isMac ? 80 : undefined } as React.CSSProperties} onDoubleClick={WindowToggleMaximise}/>
      {platform !== null && !isMac && (<div className="fixed top-1.5 right-2 z-50 flex h-7 gap-0.5 items-center">
        <button onClick={WindowMinimise} className={btn} style={noDrag} aria-label="Minimize">
          <Minus className="w-3.5 h-3.5"/>
        </button>
        <button onClick={WindowToggleMaximise} className={btn} style={noDrag} aria-label="Maximize">
          <Maximize className="w-3.5 h-3.5"/>
        </button>
        <button onClick={Quit} className={`${btn} hover:bg-destructive hover:text-white`} style={noDrag} aria-label="Close">
          <X className="w-3.5 h-3.5"/>
        </button>
      </div>)}
    </>);
}
