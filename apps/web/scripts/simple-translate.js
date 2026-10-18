/**
 * Simple Translation Script
 *
 * Goes through all locale files, finds untranslated lines,
 * translates them with Ollama line by line, and saves the results.
 */

import path from "path";
import fs from "fs";
import { Ollama } from "@langchain/ollama";

// Language mapping
const languages = {
  "ar-SY": "Arabic",
  "be-BY": "Belarusian",
  "bg-BG": "Bulgarian",
  "cs-CZ": "Czech",
  "da-DK": "Danish",
  "de-CH": "German",
  "de-DE": "German",
  "el-GR": "Greek",
  "es-ES": "Spanish",
  "et-EE": "Estonian",
  "eu-ES": "Basque",
  "fa-IR": "Persian",
  "fi-FI": "Finnish",
  "fr-FR": "French",
  "ga-IE": "Irish",
  "he-IL": "Hebrew",
  "hr-HR": "Croatian",
  "hu-HU": "Hungarian",
  "id-ID": "Indonesian",
  "it-IT": "Italian",
  "ja-JP": "Japanese",
  "ka-GE": "Georgian",
  "ko-KR": "Korean",
  "lt-LT": "Lithuanian",
  "nb-NO": "Norwegian",
  "nl-NL": "Dutch",
  "pl-PL": "Polish",
  "pt-BR": "Portuguese",
  "pt-PT": "Portuguese",
  "ro-RO": "Romanian",
  "sl-SI": "Slovenian",
  "sr-Cyrl": "Serbian",
  "sr-Latn": "Serbian",
  "sv-SE": "Swedish",
  "th-TH": "Thai",
  "tr-TR": "Turkish",
  "uk-UA": "Ukrainian",
  "ur-PK": "Urdu",
  "uz-UZ": "Uzbek",
  "vi-VN": "Vietnamese",
  "yue": "Cantonese",
  "zh-CN": "Chinese",
  "zh-HK": "Chinese",
  "zh-TW": "Chinese"
};

const llm = new Ollama({
  model: "gpt-oss:20b",
  reasoning: false
});

const localesDir = "./src/i18n/locales";
const sourceLanguage = "en-US";

// Get all paths from nested object
function getAllPaths(obj, prefix = '') {
  const paths = [];
  for (const [key, value] of Object.entries(obj)) {
    const currentPath = prefix ? `${prefix}.${key}` : key;
    if (typeof value === 'object' && value !== null) {
      paths.push(...getAllPaths(value, currentPath));
    } else if (typeof value === 'string') {
      paths.push(currentPath);
    }
  }
  return paths;
}

// Get nested value
function getValue(obj, path) {
  return path.split('.').reduce((current, key) => current?.[key], obj);
}

// Set nested value
function setValue(obj, path, value) {
  const keys = path.split('.');
  const lastKey = keys.pop();
  const target = keys.reduce((current, key) => {
    if (!current[key]) current[key] = {};
    return current[key];
  }, obj);
  target[lastKey] = value;
}

// Simple translate function
async function translate(text, targetLanguage) {
  try {
    const prompt = `Translate the following UI text from English into ${targetLanguage}. Return only the translated text, with no explanations. This translation is used by a web app monitoring tool to display health check results in a clean, modern UI. Health checks include ping, HTTP, SMTP, TCP, SSL, and similar checks:

"${text}"`;

    const response = await llm.invoke(prompt);
    return response.trim().replace(/^["']|["']$/g, ''); // Remove quotes if present
  } catch (error) {
    console.error(`Translation failed: ${error.message}`);
    return text; // Return original if translation fails
  }
}

// Find untranslated content
function findUntranslated(sourceData, targetData) {
  const sourcePaths = getAllPaths(sourceData);
  const untranslated = [];

  for (const path of sourcePaths) {
    const sourceText = getValue(sourceData, path);
    const targetText = getValue(targetData, path);

    if (!sourceText || typeof sourceText !== 'string') continue;

    // Need translation if: missing, empty, or identical to source
    if (!targetText || targetText.trim() === '' || targetText === sourceText) {
      untranslated.push({
        path,
        text: sourceText
      });
    }
  }

  return untranslated;
}

// Process single language
async function processLanguage(langCode, sourceData) {
  const langName = languages[langCode];
  const filePath = path.join(localesDir, `${langCode}.json`);

  console.log(`\n🌍 Processing ${langCode} (${langName})`);

  // Load existing translations
  let targetData = {};
  if (fs.existsSync(filePath)) {
    targetData = JSON.parse(fs.readFileSync(filePath, 'utf-8'));
  }

  // Find what needs translation
  const untranslated = findUntranslated(sourceData, targetData);

  if (untranslated.length === 0) {
    console.log(`✅ Already complete`);
    return;
  }

  console.log(`📝 Found ${untranslated.length} untranslated items`);

  // Translate each item
  for (let i = 0; i < untranslated.length; i++) {
    const item = untranslated[i];
    console.log(`[${i + 1}/${untranslated.length}] ${item.path}: "${item.text}"`);

    const translation = await translate(item.text, langName);
    setValue(targetData, item.path, translation);

    console.log(`   → "${translation}"`);

    // Small delay to avoid overwhelming the API
    await new Promise(resolve => setTimeout(resolve, 100));
  }

  // Save the file
  fs.writeFileSync(filePath, JSON.stringify(targetData, null, 2), 'utf-8');
  console.log(`💾 Saved ${langCode}.json`);
}

// Main function
async function main() {
  console.log("🚀 Simple Translation Script");

  // Load source language
  const sourceFile = path.join(localesDir, `${sourceLanguage}.json`);
  const sourceData = JSON.parse(fs.readFileSync(sourceFile, 'utf-8'));
  console.log(`📂 Loaded ${sourceLanguage} with ${getAllPaths(sourceData).length} keys`);

  // Get all target languages
  const allFiles = fs.readdirSync(localesDir);
  const targetLanguages = allFiles
    .filter(file => file.endsWith('.json'))
    .map(file => file.replace('.json', ''))
    .filter(lang => lang !== sourceLanguage && languages[lang]);

  console.log(`🎯 Processing ${targetLanguages.length} languages`);

  // Process each language
  for (const langCode of targetLanguages) {
    try {
      await processLanguage(langCode, sourceData);
    } catch (error) {
      console.error(`❌ Failed ${langCode}: ${error.message}`);
    }
  }

  console.log(`\n🏁 Done!`);
}

main().catch(console.error);
