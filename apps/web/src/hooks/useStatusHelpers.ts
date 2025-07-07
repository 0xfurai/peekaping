import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const useStatusHelpers = () => {
  const { t } = useLocalizedTranslation();

  const getStatusText = (status: number | undefined) => {
    if (typeof status === 'undefined') return t('common.unknown');

    switch (status) {
      case 0:
        return t('common.down');
      case 1:
        return t('common.up');
      case 2:
        return t('common.unknown');
      case 3:
        return t('common.maintenance');
      default:
        return t('common.unknown');
    }
  };

  const getStatusClass = (status: number | undefined) => {
    switch (status) {
      case 0:
        return "bg-red-500 border-red-600";
      case 1:
        return "bg-green-500 border-green-600";
      case 2:
        return "bg-gray-500 border-gray-600";
      case 3:
        return "bg-blue-500 border-blue-600";
      default:
        return "bg-gray-500 border-gray-600";
    }
  };

  const getStatusVariant = (status: number | undefined) => {
    switch (status) {
      case 0:
        return "destructive";
      case 1:
        return "default";
      case 2:
        return "outline";
      case 3:
        return "secondary";
      default:
        return "outline";
    }
  };

  return {
    getStatusText,
    getStatusClass,
    getStatusVariant,
  };
};
