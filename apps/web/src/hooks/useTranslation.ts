import { useTranslation } from 'react-i18next';

export const useLocalizedTranslation = () => {
  const { t, i18n } = useTranslation();

  const changeLanguage = (language: string) => {
    i18n.changeLanguage(language);
  };

  const getCurrentLanguage = () => i18n.language;

  const getAvailableLanguages = () => Object.keys(i18n.services.resourceStore.data);

  return {
    t,
    changeLanguage,
    getCurrentLanguage,
    getAvailableLanguages,
    i18n,
  };
};
