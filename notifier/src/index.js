const express = require('express');
const app = express();
const PORT = process.env.PORT || 8081;

app.use(express.json());

// Example endpoint to receive review results from the Go Backend
app.post('/api/notify', (req, res) => {
    const { repo_id, title, assignee, issue_type } = req.body;
    console.log(`[Notifier] Received notification request for repo ${repo_id}`);
    
    // TODO: Implement Windows COM object invocation here
    // Example: using win32ole or edge-js depending on exactly what COM library is needed
    // const win32ole = require('win32ole');
    // var xl = win32ole.client.Dispatch('Excel.Application');
    
    console.log(`[Notifier] Simulating Windows COM notification to ${assignee} for issue: ${title}`);

    res.status(200).json({ success: true, message: 'Notification triggered via Windows COM' });
});

app.listen(PORT, () => {
    console.log(`[Notifier] Service listening on port ${PORT} (Windows Host)`);
});
