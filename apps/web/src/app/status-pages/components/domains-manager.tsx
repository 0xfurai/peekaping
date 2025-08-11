import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { PlusIcon, XIcon, AlertTriangleIcon } from "lucide-react";

const DomainsManager = ({
  value = [],
  onChange,
}: {
  value?: string[];
  onChange: (domains: string[]) => void;
}) => {
  const { t } = useLocalizedTranslation();
  const [newDomain, setNewDomain] = useState("");
  const currentHost = window.location.hostname;

  const addDomain = () => {
    if (newDomain.trim() && !value.includes(newDomain.trim())) {
      onChange([...value, newDomain.trim()]);
      setNewDomain("");
    }
  };

  const removeDomain = (index: number) => {
    const updated = value.filter((_, i) => i !== index);
    onChange(updated);
  };

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault();
      addDomain();
    }
  };

  return (
    <div className="space-y-3">
      {value.length > 0 && (
        <div className="space-y-2">
          <p className="text-sm font-medium">{t("forms.placeholders.domains")}</p>
          <div className="space-y-2">
            {value.map((domain, index) => (
              <div key={index} className="space-y-1">
                <div className="flex items-center justify-between bg-muted p-2 rounded-md">
                  <span className="text-sm">{domain}</span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => removeDomain(index)}
                    className="h-6 w-6 p-0"
                  >
                    <XIcon className="h-3 w-3" />
                  </Button>
                </div>
                {domain === currentHost && (
                  <Alert variant="destructive" className="mt-1">
                    <AlertTriangleIcon className="h-4 w-4" />
                    <AlertDescription>
                      {t("status_pages.domain_host_warning")}
                    </AlertDescription>
                  </Alert>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex gap-2">
        <Input
          placeholder={t("forms.placeholders.domains")}
          value={newDomain}
          onChange={(e) => setNewDomain(e.target.value)}
          onKeyDown={handleKeyPress}
          className="flex-1"
        />
        <Button
          type="button"
          onClick={addDomain}
          disabled={!newDomain.trim() || value.includes(newDomain.trim())}
        >
          <PlusIcon className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
};

export default DomainsManager;