{
					__sveltekit_1oo58s9 = {
						base: ""
					};

					const element = document.currentScript.parentElement;

					Promise.all([
						import("/_app/immutable/entry/start.DGROMnvn.js"),
						import("/_app/immutable/entry/app.DTs5UHEj.js")
					]).then(([kit, app]) => {
						kit.start(app, element);
					});
				}
			