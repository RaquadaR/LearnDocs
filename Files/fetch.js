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



//*******"**

const axios = require('axios');
const fs = require('fs').promises;
const path = require('path');
const moment = require('moment');

// Configuration
const SPLUNK_BASE_URL = 'YOUR_SPLUNK_BASE_URL'; // Replace with your Splunk base URL (e.g., https://your-splunk:8089)
const SPLUNK_USERNAME = 'YOUR_SPLUNK_USERNAME'; // Replace with your Splunk username
const SPLUNK_PASSWORD = 'YOUR_SPLUNK_PASSWORD'; // Replace with your Splunk password
const SEARCH_QUERY = 'YOUR_SPLUNK_SEARCH_QUERY'; // Replace with your Splunk search query.  e.g., 'index=main'
const FETCH_INTERVAL_HOURS = 4;
const OUTPUT_FILE = 'splunk_logs.json';
const MAX_RETRY_ATTEMPTS = 3; // Maximum retry attempts for failed requests
const RETRY_DELAY_MS = 5000;    // Initial retry delay in milliseconds

/**
 * Makes a request to the Splunk API with retry logic.
 *
 * @param {string} endpoint - The Splunk API endpoint.
 * @param {string} method - The HTTP method (default: 'POST').
 * @param {object} data - The data to send with the request (for POST requests).
 * @param {number} retryCount - The current retry count (used internally).
 * @returns {Promise<any>} - A promise that resolves with the response data or rejects with an error.
 */
async function splunkRequest(endpoint, method = 'POST', data = {}, retryCount = 0) {
    try {
        const response = await axios({
            method: method,
            url: `${SPLUNK_BASE_URL}/services/${endpoint}`,
            auth: {
                username: SPLUNK_USERNAME,
                password: SPLUNK_PASSWORD,
            },
            data: new URLSearchParams(data).toString(),
            headers: {
                'Content-Type': 'application/x-www-form-urlencoded',
            },
            httpsAgent: new (require('https').Agent)({ rejectUnauthorized: false }), // Only disable for self-signed certificates in dev
        });
        return response.data;
    } catch (error) {
        console.error(`Splunk API error (${endpoint}), attempt ${retryCount + 1}:`, error.response ? error.response.data : error.message);
        if (retryCount < MAX_RETRY_ATTEMPTS) {
            console.log(`Retrying in ${RETRY_DELAY_MS}ms...`);
            await new Promise(resolve => setTimeout(resolve, RETRY_DELAY_MS));
            return splunkRequest(endpoint, method, data, retryCount + 1); // Recursive call with incremented retryCount
        } else {
            //Wrap the error in a try catch to avoid unhandled rejection
            try{
                throw error; // Re-throw the error after max retries
            }catch(e){
                console.error("Max retries reached, failing...");
                throw e;
            }

        }
    }
}

/**
 * Creates a Splunk search job.
 *
 * @param {string} searchQuery - The Splunk search query.
 * @param {string} earliestTime - The earliest time for the search.
 * @param {string} latestTime - The latest time for the search.
 * @returns {Promise<string>} - A promise that resolves with the search ID (SID).
 */
async function createSearchJob(searchQuery, earliestTime, latestTime) {
    const data = {
        search: `search ${searchQuery}`,
        earliest_time: earliestTime,
        latest_time: latestTime,
        output_mode: 'json',
    };
    const response = await splunkRequest('search/jobs', 'POST', data);
    return response.sid;
}

/**
 * Retrieves the results of a Splunk search job.  Handles pagination.
 *
 * @param {string} sid - The search ID.
 * @returns {Promise<any[]>} - A promise that resolves with an array of log events.
 */
async function getJobResults(sid) {
    let offset = 0;
    const limit = 100;
    let allResults = [];

    while (true) {
        const data = await splunkRequest(`search/jobs/${sid}/results?output_mode=json&offset=${offset}&count=${limit}`, 'GET');
        if (data.results && data.results.length > 0) {
            allResults = allResults.concat(data.results);
            offset += limit;
        } else {
            break;
        }
    }
    return allResults;
}

/**
 * Checks the status of a Splunk search job.
 *
 * @param {string} sid - The search ID.
 * @returns {Promise<string>} - A promise that resolves with the job status.
 */
async function checkJobStatus(sid) {
    const data = await splunkRequest(`search/jobs/${sid}`, 'GET');
    return data.entry[0].content['dispatchState'];
}

/**
 * Fetches logs for a specific time interval.
 *
 * @param {string} startDate - The start date/time for the interval (ISO string).
 * @param {string} endDate - The end date/time for the interval (ISO string).
 * @returns {Promise<any[]>} - A promise that resolves with an array of log events for the interval.
 */
async function fetchLogsForInterval(startDate, endDate) {
    const earliestTime = moment(startDate).utc().format();
    const latestTime = moment(endDate).utc().format();
    console.log(`Fetching logs for interval: ${earliestTime} to ${latestTime}`);

    let sid;
    try {
        sid = await createSearchJob(SEARCH_QUERY, earliestTime, latestTime);
        console.log(`Job created with SID: ${sid}`);
    } catch (error) {
        console.error("Failed to create search job:", error);
        return []; // Return empty array on job creation failure to avoid blocking other intervals
    }


    let status = await checkJobStatus(sid);
    while (status !== 'DONE' && status !== 'FAILED' && status !== 'CANCELED') {
        console.log(`Job ${sid} status: ${status}`);
        await new Promise(resolve => setTimeout(resolve, 5000)); // Check status every 5 seconds
        status = await checkJobStatus(sid);
    }

    if (status === 'DONE') {
        console.log(`Job ${sid} completed. Fetching results...`);
        try{
            const results = await getJobResults(sid);
            return results;
        }catch(error){
            console.error("Failed to get job results", error);
            return [];
        }

    } else {
        console.error(`Job ${sid} failed with status: ${status}`);
        return []; // Return empty array on job failure to avoid blocking other intervals
    }
}

/**
 * Fetches Splunk logs for a given date range in 4-hour intervals, consolidates the results, and saves them to a JSON file.
 *
 * @param {string} startDateStr - The start date (YYYY-MM-DD).
 * @param {string} endDateStr - The end date (YYYY-MM-DD).
 * @returns {Promise<void>}
 */
async function fetchLogsForMultipleDays(startDateStr, endDateStr) {
    const startDate = moment.utc(startDateStr);
    const endDate = moment.utc(endDateStr);
    const allLogs = [];

    if (!startDate.isValid() || !endDate.isValid() || startDate.isAfter(endDate)) {
        console.error('Invalid date range provided.');
        return;
    }

    let currentDate = moment(startDate);

    while (currentDate.isSameOrBefore(endDate)) {
        const nextDate = moment(currentDate).add(FETCH_INTERVAL_HOURS, 'hours');
        const intervalEndDate = nextDate.isSameOrBefore(endDate) ? nextDate : endDate;

        try {
            const logs = await fetchLogsForInterval(currentDate.toISOString(), intervalEndDate.toISOString());
            allLogs.push(...logs);
        } catch (error) {
            console.error(`Failed to fetch logs for interval ${currentDate.toISOString()} - ${intervalEndDate.toISOString()}:`, error);
            // Continue to the next interval even if one fails
        }

        currentDate.add(FETCH_INTERVAL_HOURS, 'hours');
    }

    try {
        await fs.writeFile(path.join(__dirname, OUTPUT_FILE), JSON.stringify(allLogs, null, 2));
        console.log(`Successfully fetched and saved ${allLogs.length} logs to ${OUTPUT_FILE}`);
    } catch (error) {
        console.error('Error writing to file:', error);
    }
}

// Example usage:
const requestedStartDate = '2024-07-20'; // Replace with your desired start date (YYYY-MM-DD)
const requestedEndDate = '2024-07-21';   // Replace with your desired end date (YYYY-MM-DD)

fetchLogsForMultipleDays(requestedStartDate, requestedEndDate);

