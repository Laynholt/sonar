import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { APP_VERSION } from "@/lib/version";

export function AboutPage() {
    return (<div className="space-y-6">
      <h1 className="text-2xl font-bold">About</h1>
      <Card>
        <CardHeader>
          <CardTitle>sonar v{APP_VERSION}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          UI based on SpotiDownloader (MIT)
        </CardContent>
      </Card>
    </div>);
}
