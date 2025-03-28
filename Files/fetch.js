const fs = require('fs').promises;
const path = require('path');

async function modifyPackageJson(dir, lineToAdd) {
  try {
    const files = await fs.readdir(dir);

    for (const file of files) {
      const filePath = path.join(dir, file);
      const stats = await fs.stat(filePath);

      if (stats.isDirectory()) {
        await modifyPackageJson(filePath, lineToAdd); // Recursive call for subdirectories
      } else if (file === 'package.json') {
        try {
          const content = await fs.readFile(filePath, 'utf8');
          const json = JSON.parse(content);

          // Add your logic to insert the line into the JSON object.
          // Example: adding a new script
          if (!json.scripts) {
            json.scripts = {};
          }

          const [key, value] = lineToAdd.split(':').map(str => str.trim());
          json.scripts[key] = value;

          const updatedContent = JSON.stringify(json, null, 2) + '\n'; // Add newline for better formatting
          await fs.writeFile(filePath, updatedContent, 'utf8');

          console.log(`Modified: ${filePath}`);

        } catch (jsonError) {
          console.error(`Error processing ${filePath}:`, jsonError);
        }
      }
    }
  } catch (err) {
    console.error('Error:', err);
  }
}

// Example usage:
async function main() {
  const startDir = './'; // Start from the current directory
  const lineToAdd = 'myScript: echo "Hello, world!"'; // The line to add (key: value)

  await modifyPackageJson(startDir, lineToAdd);
}

main();

