import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

// Import language files
import enUS from "./locales/en-US.json";
import frFR from "./locales/fr-FR.json";
import ukUA from "./locales/uk-UA.json";
import zhCN from "./locales/zh-CN.json";
import bgBG from "./locales/bg-BG.json";
import beBY from "./locales/be-BY.json";
import deDE from "./locales/de-DE.json";
import deCH from "./locales/de-CH.json";
import nlNL from "./locales/nl-NL.json";
import nbNO from "./locales/nb-NO.json";
import esES from "./locales/es-ES.json";
import euES from "./locales/eu-ES.json";
import faIR from "./locales/fa-IR.json";
import ptPT from "./locales/pt-PT.json";
import ptBR from "./locales/pt-BR.json";
import fiFI from "./locales/fi-FI.json";
import heIL from "./locales/he-IL.json";
import huHU from "./locales/hu-HU.json";
import hrHR from "./locales/hr-HR.json";
import itIT from "./locales/it-IT.json";
import idID from "./locales/id-ID.json";
import jaJP from "./locales/ja-JP.json";
import daDK from "./locales/da-DK.json";
import srCyrl from "./locales/sr-Cyrl.json";
import srLatn from "./locales/sr-Latn.json";
import slSI from "./locales/sl-SI.json";
import svSE from "./locales/sv-SE.json";
import trTR from "./locales/tr-TR.json";
import koKR from "./locales/ko-KR.json";
import ltLT from "./locales/lt-LT.json";
import ruRU from "./locales/ru-RU.json";
import zhHK from "./locales/zh-HK.json";
import csCZ from "./locales/cs-CZ.json";
import arSY from "./locales/ar-SY.json";
import plPL from "./locales/pl-PL.json";
import etEE from "./locales/et-EE.json";
import viVN from "./locales/vi-VN.json";
import zhTW from "./locales/zh-TW.json";
import thTH from "./locales/th-TH.json";
import elGR from "./locales/el-GR.json";
import yue from "./locales/yue.json";
import roRO from "./locales/ro-RO.json";
import urPK from "./locales/ur-PK.json";
import kaGE from "./locales/ka-GE.json";
import uzUZ from "./locales/uz-UZ.json";
import gaIE from "./locales/ga-IE.json";

const resources = {
  "en-US": { translation: enUS }, // main
  "ar-SY": { translation: arSY }, // done
  "cs-CZ": { translation: csCZ }, // done
  "zh-HK": { translation: zhHK }, // done
  "bg-BG": { translation: bgBG }, // done
  "be-BY": { translation: beBY }, // done
  "de-DE": { translation: deDE }, // done
  "de-CH": { translation: deCH }, // done
  "nl-NL": { translation: nlNL }, // done
  "nb-NO": { translation: nbNO }, // done
  "es-ES": { translation: esES }, // done
  "eu-ES": { translation: euES }, // done
  "fa-IR": { translation: faIR }, // done
  "pt-PT": { translation: ptPT }, // done
  "pt-BR": { translation: ptBR }, // done
  "fi-FI": { translation: fiFI }, // done
  "fr-FR": { translation: frFR }, // done
  "he-IL": { translation: heIL }, // done
  "hu-HU": { translation: huHU }, // done
  "hr-HR": { translation: hrHR }, // done
  "it-IT": { translation: itIT }, // done
  "id-ID": { translation: idID }, // done
  "ja-JP": { translation: jaJP }, // done
  "da-DK": { translation: daDK }, // done
  "sr-Cyrl": { translation: srCyrl },
  "sr-Latn": { translation: srLatn }, // done
  "sl-SI": { translation: slSI }, // done
  "sv-SE": { translation: svSE }, // done
  "tr-TR": { translation: trTR }, // done
  "ko-KR": { translation: koKR }, // done
  "lt-LT": { translation: ltLT }, // done
  "ru-RU": { translation: ruRU }, // done
  "zh-CN": { translation: zhCN }, // done
  "pl-PL": { translation: plPL }, // done
  "et-EE": { translation: etEE }, // done
  "vi-VN": { translation: viVN }, // done
  "zh-TW": { translation: zhTW }, // done
  "uk-UA": { translation: ukUA }, // done
  "th-TH": { translation: thTH }, // done
  "el-GR": { translation: elGR }, // done
  yue: { translation: yue },
  "ro-RO": { translation: roRO },
  "ur-PK": { translation: urPK },
  "ka-GE": { translation: kaGE },
  "uz-UZ": { translation: uzUZ },
  "ga-IE": { translation: gaIE },
};

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: "en-US",
    debug: false,
    interpolation: {
      escapeValue: false, // React already escapes values
    },
    detection: {
      order: ["localStorage", "navigator", "htmlTag"],
      caches: ["localStorage"],
    },
  });

export default i18n;
