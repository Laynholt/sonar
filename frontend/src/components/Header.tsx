import { Badge } from "@/components/ui/badge";
import { APP_VERSION } from "@/lib/version";

export function Header() {
    return (<div className="text-center">
      <div className="flex items-center justify-center gap-3">
        <h1 className="text-4xl font-bold cursor-pointer" onClick={() => window.location.reload()}>
          sonar
        </h1>
        <Badge variant="default">v{APP_VERSION}</Badge>
      </div>
    </div>);
}
