<!DOCTYPE html>
<html>
<head>
    <title>{{.Status}} {{.Reason}}</title>
</head>
<body>
    <h1>{{.Status}} {{.Reason}}</h1>
    <p>An error occured:</p>
    <pre>{{.Error}}</pre>
    <hr>
    <address>{{.ServerName}} at {{.ListenOn}}</address>
</body>
</html>
