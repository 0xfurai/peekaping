import { useTranslation } from 'react-i18next';
import { loadLanguage, AVAILABLE_LANGUAGES } from '../i18n';

export const useLocalizedTranslation = () => {
  const { t, i18n } = useTranslation();

  const changeLanguage = async (language: string) => {
    try {
      // Load the language dynamically if not already loaded
      await loadLanguage(language);
      // Change the language after it's loaded
      await i18n.changeLanguage(language);
    } catch (error) {
      console.error(`Failed to change language to ${language}:`, error);
      // Fallback to English if loading fails
      await i18n.changeLanguage('en-US');
    }
  };

  const getCurrentLanguage = () => i18n.language;

  const getAvailableLanguages = () => AVAILABLE_LANGUAGES;

  return {
    t,
    changeLanguage,
    getCurrentLanguage,
    getAvailableLanguages,
    i18n,
  };
};
