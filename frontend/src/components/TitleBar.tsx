import { X, Minus, Maximize } from "lucide-react";
import { WindowMinimise, WindowToggleMaximise, Quit } from "../../wailsjs/runtime/runtime";

const noDrag = { "--wails-draggable": "no-drag" } as React.CSSProperties;
const btn = "w-8 h-7 flex items-center justify-center hover:bg-muted transition-colors rounded";

export function TitleBar() {
    return (<>
      <div className="fixed top-0 left-0 right-0 h-10 z-40 bg-background/80 backdrop-blur-sm flex items-center px-4 text-sm font-semibold" style={{ "--wails-draggable": "drag" } as React.CSSProperties} onDoubleClick={WindowToggleMaximise}>
        sonar
      </div>
      <div className="fixed top-1.5 right-2 z-50 flex h-7 gap-0.5 items-center">
        <button onClick={WindowMinimise} className={btn} style={noDrag} aria-label="Свернуть">
          <Minus className="w-3.5 h-3.5"/>
        </button>
        <button onClick={WindowToggleMaximise} className={btn} style={noDrag} aria-label="Развернуть">
          <Maximize className="w-3.5 h-3.5"/>
        </button>
        <button onClick={Quit} className={`${btn} hover:bg-destructive hover:text-white`} style={noDrag} aria-label="Закрыть">
          <X className="w-3.5 h-3.5"/>
        </button>
      </div>
    </>);
}
