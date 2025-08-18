import { Button } from "@/components/ui/button";
import { ArrowLeft } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

interface BackButtonProps {
  to?: string;
  onClick?: () => void;
  className?: string;
}

export function BackButton({ to, onClick, className = "mb-4" }: BackButtonProps) {
  const navigate = useNavigate();
  const { t } = useLocalizedTranslation();

  const handleClick = () => {
    if (onClick) {
      onClick();
    } else if (to) {
      navigate(to);
    } else {
      navigate(-1);
    }
  };

  return (
    <Button
      variant="ghost"
      onClick={handleClick}
      className={className}
    >
      <ArrowLeft />
      {t("common.back")}
    </Button>
  );
}