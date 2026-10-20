import { useState } from "react";
import { ArrowRight, Copy, ExternalLink, Search } from "lucide-react";
import { toast } from "sonner";
import { TitleBar } from "@/components/TitleBar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { Find } from "../wailsjs/go/main/App";
import { main } from "../wailsjs/go/models";
import { BrowserOpenURL, ClipboardSetText } from "../wailsjs/runtime/runtime";
import { cn } from "@/lib/utils";

type Item = main.Item;

async function copy(url: string) {
    if (await ClipboardSetText(url)) toast.success("Ссылка скопирована");
    else toast.error("Не удалось скопировать");
}

function Cover({ src, className }: { src: string; className?: string }) {
    return src
        ? <img src={src} alt="" className={cn("rounded-md object-cover bg-muted shrink-0", className)}/>
        : <div className={cn("rounded-md bg-muted shrink-0", className)}/>;
}

function ItemCard({ label, item, status, children }: { label: string; item: Item; status?: React.ReactNode; children?: React.ReactNode }) {
    return (<Card className="flex-1 min-w-0 gap-3 p-4">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="flex gap-3 min-w-0">
        <Cover src={item.image} className="w-24 h-24"/>
        <div className="min-w-0 flex flex-col gap-1">
          <div className="font-semibold line-clamp-2 break-words" title={item.title}>
            {item.title}{item.explicit && <span className="ml-1.5 text-[10px] align-middle rounded bg-muted px-1 py-0.5 text-muted-foreground">E</span>}
          </div>
          <div className="text-sm text-muted-foreground truncate" title={item.artist}>{item.artist}</div>
          {status}
        </div>
      </div>
      {children}
    </Card>);
}

function Links({ url }: { url: string }) {
    return (<div className="flex gap-2">
      <Button size="sm" className="flex-1" onClick={() => BrowserOpenURL(url)}><ExternalLink/>Открыть</Button>
      <Button size="sm" variant="outline" onClick={() => copy(url)} aria-label="Копировать ссылку"><Copy/></Button>
    </div>);
}

export default function App() {
    const [link, setLink] = useState("");
    const [loading, setLoading] = useState(false);
    const [result, setResult] = useState<main.Result | null>(null);
    const [picked, setPicked] = useState(0);

    const find = async () => {
        if (!link.trim() || loading) return;
        setLoading(true);
        try {
            setResult(await Find(link));
            setPicked(0);
        }
        catch (e) {
            toast.error(String(e));
        }
        finally {
            setLoading(false);
        }
    };

    const matches = result?.matches ?? [];
    const best = matches[picked];

    return (<div className="h-screen flex flex-col">
      <TitleBar />
      <main className="flex-1 overflow-y-auto pt-14 pb-6 px-6 max-w-3xl w-full mx-auto flex flex-col gap-5">
        <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); void find(); }}>
          <Input autoFocus value={link} onChange={(e) => setLink(e.target.value)} placeholder="Ссылка на трек или альбом Spotify" disabled={loading}/>
          <Button type="submit" disabled={loading || !link.trim()}>
            {loading ? <Spinner/> : <Search/>}Найти
          </Button>
        </form>

        {result && (<>
          <div className="flex items-stretch gap-3">
            <ItemCard label="Spotify" item={result.source}>
              <Links url={result.source.url}/>
            </ItemCard>
            <ArrowRight className="self-center shrink-0 text-muted-foreground"/>
            {best ? (<ItemCard label="Amazon Music" item={best} status={<div className={cn("text-xs", result.confident && picked === 0 ? "text-green-600 dark:text-green-400" : "text-yellow-600 dark:text-yellow-400")}>
                  {result.confident && picked === 0 ? "Совпадение найдено" : "Совпадение неточное — проверь"}
                </div>}>
                <Links url={best.url}/>
              </ItemCard>) : (<Card className="flex-1 min-w-0 p-4 items-center justify-center text-center gap-3">
                <div className="text-sm text-muted-foreground">В Amazon Music ничего не нашлось</div>
                <Button size="sm" variant="outline" onClick={() => BrowserOpenURL(result.searchUrl)}><Search/>Искать вручную</Button>
              </Card>)}
          </div>

          {matches.length > 1 && (<div className="flex flex-col gap-1">
            <div className="text-xs uppercase tracking-wide text-muted-foreground mb-1">Другие варианты</div>
            {matches.map((m, i) => i !== picked && (<button key={m.url} onClick={() => setPicked(i)} className="flex items-center gap-3 rounded-md p-2 text-left hover:bg-muted transition-colors cursor-pointer">
                <Cover src={m.image} className="w-10 h-10"/>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium truncate">{m.title}{m.explicit && " · E"}</div>
                  <div className="text-xs text-muted-foreground truncate">{m.artist}</div>
                </div>
                <div className="text-xs text-muted-foreground tabular-nums">{Math.round(m.score * 100)}%</div>
              </button>))}
          </div>)}
        </>)}
      </main>
    </div>);
}
