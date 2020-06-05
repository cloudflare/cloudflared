package websocket

func nonWebSocketRequestPage() []byte {
	return []byte(`<!DOCTYPE html>
		<html lang="en">
		
		<head>
			<meta charset="UTF-8">
			<meta name="robots" content="noindex">
			<meta name="viewport" content="initial-scale=1, maximum-scale=1, user-scalable=no, width=device-width">
			<title>Cloudflare Access</title>
		
			<style>
				body {
					margin: 0;
					padding: 0;
					border: 0;
					font-size: 16px;
					line-height: 1.7em;
					font-family: "Open Sans", sans-serif;
					color: #424242;
					background: #f3f1fe;
				}
		
				.main-title {
					font-size: 30px;
					color: #fff;
					padding: 40px;
					background: rgb(129, 118, 181);
					background: linear-gradient(72deg, rgba(129, 118, 181, 1) 0%, rgba(127, 120, 183, 1) 35%, rgba(119, 195, 224, 1) 100%);
				}
		
				.main-content {
					padding: 0;
					margin: 0
				}
		
				.section-1 {
					display: flex;
					flex-direction: row;
					padding: 40px;
					box-shadow: 0 4px 9px 1px rgba(129, 118, 181, 0.25);
					background: #fff;
					flex-wrap: wrap;
		
					justify-content: space-between;
				}
		
				.logo-section {
					padding: 20px;
					display: flex;
					justify-content: center;
					padding-right: 40px;
				}
		
				.logo-section > div {
					margin: auto;
				}
		
				.logo {
					width: auto;
					height: auto;
					min-width: 250px;
				}
		
				.section-1-content {
					display: flex;
					flex-direction: row;
					flex-wrap: wrap;
					margin: 40px;
					justify-content: space-between;
					flex-basis: calc(70% - 100px);
					max-width: calc(70% - 100px);
					border-right: 1px solid #d8d0ff;
				}
		
				.section-1-content .main-message {
					flex-basis: calc(49% - 40px);
					max-width: calc(49% - 40px);
					padding: 20px;
					display: flex;
					flex-direction: column;
					justify-content: center;
				}
		
				.section-1-content .debug-details {
					padding: 20px;
					display: flex;
					flex-direction: column;
					justify-content: center;
					color: #7e7e7e;
					flex-basis: calc(49% - 40px);
					max-width: calc(49% - 40px);
					flex-basis: calc(49% - 40px);
					max-width: calc(49% - 40px);
					align-items: center;
					justify-content: center;
					position: relative;
				}
		
				.section-1-content .main-message .title {
					font-size: 50px;
					margin-bottom: 20px;
				}
		
				.section-1-content .main-message .sub-title {
					color: #8f8f8f;
					font-size: 20px;
				}
		
				.section-2 {
					padding: 50px 100px;
					color: rgb(71, 64, 106);
					font-size: 18px;
				}
		
				.section-2 .zd-link-message {
					margin-top: 10px;
				}
		
				.section-2 .cf-link {
					margin-top: 30px;
				}
		
				.section-2 .cf-link-message {
					display: inline;
				}
		
				.section-3 {
					color: rgb(71, 64, 106);
		
					padding: 40px 80px 20px;
				}
		
				footer {
					border-top: 1px solid #d8d0ff;
					padding-top: 20px;
					text-align: center;
				}
		
				a {
					text-decoration: none;
					color: #2400cf;
				}
		
				a:visited {
					color: #3f2ba2;
				}
		
				.Message-is-warning {
					color: #cc8400;
				}
		
				.Message-is-success {
					color: #028402;
				}
		
				.appName {
					text-transform: capitalize;
				}
		
				.org-logo {
					width: auto;
					height: auto;
					max-width: 155px;
				}
		
				.watermark-logo::after {
					opacity: 0.05;
					height: 100%;
					width: 70%;
					background-repeat: no-repeat;
					background-position: center;
					content: "#";
					z-index: 1;
					position: absolute;
				}
		
				.watermark-logo .debug-text {
					display: flex;
					flex-direction: column;
					justify-items: center;
					z-index: 10;
				}
		
				.watermark-logo p, .watermark-logo div {
					opacity: 1;
				}
		
				.app-title {
					color: #5f5f5f;
				}
		
				@media only screen and (max-width: 1046px) {
					.section-1-content .main-message {
						flex-basis: 100%;
						max-width: 100%;
						padding: 20px;
					}
		
					.section-1-content .debug-details {
						flex-basis: 100%;
						max-width: 100%;
						padding: 20px;
						margin-top: 50px;
					}
		
					.logo {
						min-width: 200px;
					}
				}
		
				@media only screen and (max-width: 890px) {
					.section-1 {
						flex-wrap: wrap-reverse;
						padding: 5px;
					}
		
					.section-1-content {
						padding: 5px;
					}
		
					.section-2 {
						padding: 50px 30px;
					}
		
					.logo {
						min-width: 150px;
					}
		
					.logo-section {
						flex-basis: 100%;
						max-width: 100%;
					}
		
					.section-1-content {
						border-right: 0;
						flex-basis: 100%;
						max-width: 100%;
					}
				}
			</style>
		</head>
		
		
		<body>
		<header class="main-title">
				Cloudflare Access
		</header>
		<div class="main-content">
			<div class="section-1">
				<div class="section-1-content">
					<div class="main-message">
						<div class="title">  Success </div>
						<div class="sub-title">
						You are now logged in and can reach this application. 
							You can close this browser window.
						</div>
					</div>
				</div>
		
				<div class="logo-section">
					<div>
		
						<svg class="logo" width="250" viewBox="0 0 122 53" xmlns="http://www.w3.org/2000/svg">
							<rect class="" width="100%" height="100%" fill="none" style=""/>
							<defs>
								<style>.cls-5 {
									fill: #fff
								}
		
								.st0 {
									fill: #8176b5
								}</style>
							</defs>
							<g class="currentLayer" style="">
								<path class="" d="m113.65 12.721l-6.72-1.56-1.2-0.48-30.843 0.24v14.882l38.763 0.12z"
									  fill="#fff"/>
								<path class=""
									  d="m101.05 24.482c0.36-1.2 0.24-2.4-0.36-3.12s-1.44-1.2-2.52-1.32l-20.882-0.24c-0.12 0-0.24-0.12-0.36-0.12-0.12-0.12-0.12-0.24 0-0.36 0.12-0.24 0.24-0.36 0.48-0.36l21.002-0.24c2.52-0.12 5.16-2.16 6.12-4.56l1.2-3.12c0-0.12 0.12-0.24 0-0.36-1.32-6.121-6.84-10.682-13.32-10.682-6.001 0-11.162 3.84-12.962 9.241-1.2-0.84-2.64-1.32-4.32-1.2-2.88 0.24-5.16 2.64-5.52 5.52-0.12 0.72 0 1.44 0.12 2.16-4.681 0.12-8.521 3.961-8.521 8.761 0 0.48 0 0.84 0.12 1.32 0 0.24 0.24 0.36 0.36 0.36h38.523c0.24 0 0.48-0.12 0.48-0.36l0.36-1.32z"
									  fill="#f48120"/>
								<path class=""
									  d="m107.65 11.041h-0.6c-0.12 0-0.24 0.12-0.36 0.24l-0.84 2.88c-0.36 1.2-0.24 2.4 0.36 3.12s1.44 1.2 2.52 1.32l4.44 0.24c0.12 0 0.24 0.12 0.36 0.12 0.12 0.12 0.12 0.241 0 0.361-0.12 0.24-0.24 0.36-0.48 0.36l-4.56 0.24c-2.52 0.12-5.16 2.16-6.12 4.56l-0.24 1.08c-0.12 0.12 0 0.36 0.24 0.36h15.84c0.24 0 0.36-0.12 0.36-0.36 0.24-0.96 0.48-2.04 0.48-3.12 0-6.24-5.16-11.4-11.4-11.4"
									  fill="#faad3f"/>
								<path class="st0"
									  d="m120.61 32.643c-0.6 0-1.08-0.48-1.08-1.08s0.48-1.08 1.08-1.08 1.08 0.48 1.08 1.08-0.48 1.08-1.08 1.08m0-1.92c-0.48 0-0.84 0.36-0.84 0.84s0.36 0.84 0.84 0.84 0.84-0.36 0.84-0.84-0.36-0.84-0.84-0.84m0.48 1.44h-0.24l-0.24-0.36h-0.24v0.36h-0.24v-1.08h0.6c0.24 0 0.36 0.12 0.36 0.36 0 0.12-0.12 0.24-0.24 0.36l0.24 0.36zm-0.36-0.6c0.12 0 0.12 0 0.12-0.12s-0.12-0.12-0.12-0.12h-0.36v0.36h0.36zm-107.65-1.08h2.64v7.2h4.562v2.28h-7.2zm9.962 4.68c0-2.76 2.16-4.92 5.16-4.92s5.04 2.16 5.04 4.92-2.16 4.92-5.16 4.92c-2.88 0-5.04-2.16-5.04-4.92m7.56 0c0-1.44-0.96-2.64-2.4-2.64s-2.4 1.2-2.4 2.52 0.96 2.52 2.4 2.52c1.44 0.24 2.4-0.96 2.4-2.4m5.88 0.6v-5.28h2.64v5.28c0 1.32 0.72 2.04 1.801 2.04s1.8-0.6 1.8-1.92v-5.4h2.64v5.28c0 3.12-1.8 4.44-4.44 4.44-2.76-0.12-4.44-1.44-4.44-4.44m12.84-5.28h3.721c3.36 0 5.4 1.92 5.4 4.68s-2.04 4.8-5.4 4.8h-3.6v-9.48zm3.721 7.08c1.56 0 2.64-0.84 2.64-2.4s-1.08-2.4-2.64-2.4h-1.08v4.8h1.08zm9.12-7.08h7.561v2.28h-4.92v1.56h4.44v2.16h-4.44v3.48h-2.64zm11.282 0h2.64v7.2h4.56v2.28h-7.2zm14.04-0.12h2.641l4.08 9.6h-2.88l-0.72-1.68h-3.72l-0.72 1.68h-2.76l4.08-9.6zm2.401 5.88l-1.08-2.64-1.08 2.64h2.16zm7.68-5.76h4.441c1.44 0 2.4 0.36 3.12 1.08 0.6 0.6 0.84 1.32 0.84 2.16 0 1.44-0.72 2.4-1.92 2.88l2.28 3.36h-3l-1.92-2.88h-1.2v2.88h-2.64v-9.48zm4.321 4.56c0.84 0 1.44-0.48 1.44-1.08 0-0.72-0.6-1.08-1.44-1.08h-1.68v2.28h1.68zm7.8-4.56h7.681v2.16h-5.04v1.44h4.56v2.16h-4.56v1.44h5.16v2.28h-7.8zm-102.37 5.88a2.37 2.37 0 0 1 -2.16 1.44c-1.44 0-2.4-1.2-2.4-2.52s0.96-2.52 2.4-2.52c1.08 0 1.92 0.72 2.28 1.56h2.76c-0.48-2.28-2.4-3.96-5.04-3.96-2.88 0-5.16 2.16-5.16 4.92s2.16 4.92 5.04 4.92c2.52 0 4.44-1.68 5.04-3.84h-2.76z"
									  fill="#8176b5"/>
								<path class="st0"
									  d="m53.092 43.286h2.614l4.04 9.68h-2.852l-0.713-1.695h-3.683l-0.713 1.694h-2.733l4.04-9.68zm2.376 5.928h-2.138l1.07-2.662 1.069 2.662zm58.031-5.928c1.71 0 3.8 0.783 3.8 2.966h-2.93c0-1.281-2.371-0.985-2.123 0 0.223 0.886 1.533 0.783 2.22 0.914 2.422 0.46 3.12 1.804 3.12 2.765 0 1.353-0.64 3.216-4.087 3.216-1.854 0-4.393-0.546-4.387-3.086h2.99c0 1.223 2.494 1.332 2.494 0.13 0-0.664-1.166-1.085-2.35-1.245-0.875-0.119-2.912-0.73-2.912-2.634 0-1.048 0.457-3.026 4.165-3.026zm-11.504 0c1.71 0 3.8 0.783 3.8 2.966h-2.931c0-1.281-2.37-0.985-2.123 0 0.223 0.886 1.534 0.783 2.22 0.914 2.422 0.46 3.12 1.804 3.12 2.765 0 1.353-0.639 3.216-4.086 3.216-1.854 0-4.394-0.546-4.387-3.086h2.99c0 1.223 2.494 1.332 2.494 0.13 0-0.664-1.167-1.085-2.35-1.245-0.876-0.119-2.913-0.73-2.913-2.634 0-1.048 0.458-3.026 4.166-3.026zm-15.05 0.202h7.406v2.225h-4.877v1.433h4.417v2.073h-4.417v1.5h4.942v2.226h-7.47v-9.457zm-18.326 5.702l2.692 0.016c-0.492 2.158-2.397 3.776-4.827 3.776-2.84 0-4.942-2.174-4.942-4.888v-0.034c0-2.714 2.135-4.922 4.975-4.922 2.496 0 4.433 1.669 4.86 3.928h-2.693c-0.328-0.91-1.133-1.568-2.183-1.568-1.396 0-2.332 1.163-2.332 2.529v0.033c0 1.349 0.952 2.546 2.348 2.546 0.985 0 1.74-0.59 2.102-1.416zm12.326 0l2.693 0.016c-0.493 2.158-2.397 3.776-4.828 3.776-2.84 0-4.942-2.174-4.942-4.888v-0.034c0-2.714 2.135-4.922 4.975-4.922 2.496 0 4.433 1.669 4.86 3.928h-2.691c-0.329-0.91-1.133-1.568-2.184-1.568-1.396 0-2.332 1.163-2.332 2.529v0.033c0 1.349 0.953 2.546 2.348 2.546 0.985 0 1.74-0.59 2.102-1.416z"
									  fill="#8176b5" fill-rule="evenodd"/>
							</g>
						</svg>
		
					</div>
				</div>
			</div>
			<div class="section-2">
			</div>
			<div class="section-3 ">
				<footer>
					<a href="https://support.cloudflare.com/hc/en-us" target="_blank"> Help </a>
					•
					<span>Performance &amp; Security by
				  <a href="https://www.cloudflare.com/products/cloudflare-access/" target="_blank">Cloudflare Access</a>
				</span>
				</footer>
			</div>
		</div>
		</body>
		
		</html>`)
}
