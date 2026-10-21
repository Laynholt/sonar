import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { APP_VERSION } from "@/lib/version";
import { useI18n } from "@/lib/i18n";

export function AboutPage() {
    const { t } = useI18n();
    return (<div className="space-y-6">
      <h1 className="text-2xl font-bold">{t("nav.about")}</h1>
      <Card>
        <CardHeader>
          <CardTitle>sonar v{APP_VERSION}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          {t("about.credit")}
        </CardContent>
      </Card>
    </div>);
}
